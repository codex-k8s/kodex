package kubernetesoperation

import (
	"bytes"
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// InputRelay performs one frame's owner admission, joined write and evidence delivery without retries.
// InputRelay выполняет owner admission, joined write и evidence delivery одного frame без retries.
type InputRelay struct {
	owner targetrepo.FrameOwner
	now   func() time.Time
}

// NewInputRelay accepts only the protected owner port; construction grants no stream authority.
// NewInputRelay принимает только protected owner port; создание не выдаёт stream authority.
func NewInputRelay(owner targetrepo.FrameOwner) (*InputRelay, error) {
	if owner == nil {
		return nil, targetrepo.ErrFrameUnavailable
	}
	return &InputRelay{owner: owner, now: time.Now}, nil
}

// Send snapshots bounded bytes before owner I/O and never resends UNKNOWN or acknowledged input.
// A true attempted result means the outer stream must retain uncertainty if no ACK was returned.
// Local ACK proves a joined transport write, not execution or exactly-once effects on the remote host.
// Send фиксирует bounded bytes до owner I/O, не повторяя UNKNOWN либо acknowledged input.
// True attempted требует сохранить uncertainty во внешнем stream при отсутствии возвращённого ACK.
// Local ACK доказывает joined transport write, не execution либо exactly-once effects удалённого узла.
func (relay *InputRelay) Send(ctx context.Context, binding managedkubernetes.Binding, grant value.KubernetesStreamGrant, frame value.KubernetesStreamFrame, writer targetrepo.FrameWriter) (acknowledged uint64, attempted bool, resultErr error) {
	if relay == nil || relay.owner == nil || writer == nil || ctx == nil || ctx.Err() != nil || binding.Validate() != nil ||
		binding.InvocationRef == "" || binding.ConnectionTestRef != "" || binding.Generation != grant.Generation ||
		len(frame.Payload) > managedkubernetes.MaximumFrameBytes {
		return 0, false, targetrepo.ErrFrameUnavailable
	}
	frame.Payload = bytes.Clone(frame.Payload)
	defer clear(frame.Payload)
	if !frame.ValidInput(grant, relay.now()) {
		return 0, false, targetrepo.ErrFrameUnavailable
	}
	var opening *managedkubernetes.PortForwardConnectionIdentity
	var forwardWriter targetrepo.PortForwardOpenWriter
	if frame.Channel == "PORT_FORWARD" {
		forward, decodeErr := managedkubernetes.DecodePortForwardConnectionFrame(frame.Payload)
		if decodeErr != nil || forward.ValidateInput(managedkubernetes.MaximumForwardPorts) != nil {
			return 0, false, targetrepo.ErrFrameUnavailable
		}
		if forward.Action == managedkubernetes.PortForwardConnectionOpen {
			var available bool
			forwardWriter, available = writer.(targetrepo.PortForwardOpenWriter)
			if !available {
				return 0, false, targetrepo.ErrFrameUnavailable
			}
			opening = &managedkubernetes.PortForwardConnectionIdentity{ConnectionRef: forward.ConnectionRef, Generation: forward.Generation, Slot: forward.Slot}
		}
	}
	metadata := value.KubernetesFrameMetadata{StreamRef: grant.Ref, Direction: "INPUT", Channel: frame.Channel,
		Sequence: frame.Sequence, PayloadDigest: frame.PayloadDigest, ByteCount: uint32(len(frame.Payload)), EndOfInput: frame.EndOfInput}
	admission, err := relay.owner.BeginKubernetesFrame(ctx, binding, metadata)
	if err != nil || admission.DeliveryOrdinal != 0 {
		return 0, false, targetrepo.ErrFrameUnavailable
	}
	if admission.PortForwardRetention != nil && (opening == nil || admission.Disposition != "SEND") {
		return 0, false, targetrepo.ErrFrameUnavailable
	}
	switch admission.Disposition {
	case "ACKNOWLEDGED":
		return frame.Sequence, false, nil
	case "SEND":
		if ctx.Err() != nil || !admission.ExpiresAt.After(relay.now()) || admission.ExpiresAt.After(grant.ExpiresAt) {
			return 0, false, targetrepo.ErrFrameUnavailable
		}
		if opening != nil && (admission.PortForwardRetention == nil || admission.PortForwardRetention.Validate(managedkubernetes.MaximumForwardPorts, opening) != nil) {
			return 0, false, targetrepo.ErrFrameUnavailable
		}
	default:
		return 0, false, targetrepo.ErrFrameUnavailable
	}
	work, cancel := context.WithDeadline(ctx, admission.ExpiresAt)
	if opening != nil {
		err = forwardWriter.WritePortForwardOpen(work, frame, admission.PortForwardRetention.Clone())
	} else {
		err = writer.WriteFrame(work, frame)
	}
	cancel()
	if err != nil {
		return 0, true, targetrepo.ErrFrameUnavailable
	}
	evidence, finish := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer finish()
	acknowledged, err = relay.owner.CompleteKubernetesFrame(evidence, binding, metadata)
	if err != nil || acknowledged != frame.Sequence {
		return 0, true, targetrepo.ErrFrameUnavailable
	}
	return acknowledged, true, nil
}
