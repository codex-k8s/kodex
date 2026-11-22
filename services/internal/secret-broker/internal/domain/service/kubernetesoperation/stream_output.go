package kubernetesoperation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

const (
	// maximumOutputDeliveries bounds retained-buffer retries; the owner independently enforces the same cap.
	// maximumOutputDeliveries ограничивает retained-buffer retries; owner независимо проверяет тот же предел.
	maximumOutputDeliveries = managedkubernetes.MaximumOutputDeliveries
	// outputOwnerTimeout bounds each protected receipt/admission call and permits prompt local cleanup.
	// outputOwnerTimeout ограничивает каждый protected receipt/admission call для своевременного cleanup.
	outputOwnerTimeout = 3 * time.Second
)

// OutputWindow retains exactly one private output frame until its explicit gateway acknowledgement.
// It never re-reads a target or restarts a process. Every delivery, including a retained-byte retry,
// needs fresh owner credit; the surrounding stream service must also recheck identity TTL and target
// authorization. This buffer owns no remote authority, durable transcript or recovery mechanism.
// OutputWindow хранит ровно один private output frame до явного gateway acknowledgement.
// Не перечитывает target и не перезапускает процесс. Каждая доставка, включая retained-byte retry,
// требует fresh owner credit; внешний stream service также проверяет identity TTL и target
// authorization. Buffer не владеет remote authority, durable transcript либо recovery mechanism.
type OutputWindow struct {
	owner                                                   targetrepo.FrameOwner
	binding                                                 managedkubernetes.Binding
	grant                                                   value.KubernetesStreamGrant
	access                                                  chan struct{}
	now                                                     func() time.Time
	frame                                                   value.KubernetesStreamFrame
	nextSequence, acknowledged, stagedBytes, deliveredBytes uint64
	deliveries, deliveryOrdinal                             uint32
	delivered, acknowledgementPending, closed               bool
}

// NewOutputWindow pins the original claim and clones channel sets; it grants no right to read a target.
// NewOutputWindow закрепляет original claim и копирует channel sets; права чтения target не выдаёт.
func NewOutputWindow(owner targetrepo.FrameOwner, binding managedkubernetes.Binding, grant value.KubernetesStreamGrant) (*OutputWindow, error) {
	if owner == nil || binding.Validate() != nil || binding.InvocationRef == "" || binding.Generation != grant.Generation || !grant.Valid(time.Now()) || len(grant.OutputChannels) == 0 {
		return nil, targetrepo.ErrFrameUnavailable
	}
	grant.InputChannels = slices.Clone(grant.InputChannels)
	grant.OutputChannels = slices.Clone(grant.OutputChannels)
	window := &OutputWindow{owner: owner, binding: binding, grant: grant, access: make(chan struct{}, 1), now: time.Now}
	window.access <- struct{}{}
	return window, nil
}

// acquire bounds contention by the caller context instead of leaving cancelled exchanges queued.
// acquire ограничивает ожидание caller context вместо очереди отменённых exchanges.
func (window *OutputWindow) acquire(ctx context.Context) bool {
	if window == nil || ctx == nil || ctx.Err() != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case <-window.access:
		return true
	}
}

// release returns exclusive access after all private payload and receipt operations have joined.
// release возвращает exclusive access после завершения private payload и receipt operations.
func (window *OutputWindow) release() { window.access <- struct{}{} }

// Stage snapshots one already-read bounded chunk without admitting a new target read or overwriting output.
// Empty chunks do not create sequences; terminal process receipts use the separate owner lifecycle.
// Stage фиксирует уже прочитанный bounded chunk без допуска нового target read либо перезаписи output.
// Пустые chunks не создают sequence; terminal process receipts используют отдельный owner lifecycle.
func (window *OutputWindow) Stage(ctx context.Context, channel string, payload []byte) error {
	if !window.acquire(ctx) {
		return targetrepo.ErrFrameUnavailable
	}
	defer window.release()
	if ctx.Err() != nil || window.closed || !window.grant.Valid(window.now()) || window.frame.Sequence != 0 ||
		!slices.Contains(window.grant.OutputChannels, channel) || len(payload) == 0 || len(payload) > int(window.grant.MaximumFrameBytes) ||
		window.stagedBytes+uint64(len(payload)) > window.grant.MaximumTotalBytes || window.nextSequence >= managedkubernetes.MaximumFrameSequence {
		return targetrepo.ErrFrameUnavailable
	}
	if channel == "PORT_FORWARD" {
		if _, err := managedkubernetes.PortForwardOutputWitness(payload); err != nil {
			return targetrepo.ErrFrameUnavailable
		}
	}
	owned := bytes.Clone(payload)
	digest := sha256.Sum256(owned)
	window.nextSequence++
	window.stagedBytes += uint64(len(owned))
	window.frame = value.KubernetesStreamFrame{Sequence: window.nextSequence, Channel: channel, Payload: owned, PayloadDigest: hex.EncodeToString(digest[:])}
	window.deliveries = 0
	window.deliveryOrdinal = 0
	window.delivered = false
	window.acknowledgementPending = false
	return nil
}

// metadata exposes only the exact immutable intent of the retained frame to the owner.
// metadata передаёт owner только exact immutable intent сохранённого frame.
func (window *OutputWindow) metadata() value.KubernetesFrameMetadata {
	metadata := value.KubernetesFrameMetadata{StreamRef: window.grant.Ref, Direction: "OUTPUT", Channel: window.frame.Channel, PayloadDigest: window.frame.PayloadDigest,
		Sequence: window.frame.Sequence, ByteCount: uint32(len(window.frame.Payload))}
	if window.frame.Channel == "PORT_FORWARD" {
		witness, err := managedkubernetes.PortForwardOutputWitness(window.frame.Payload)
		if err == nil {
			metadata.PortForward = &witness
		}
	}
	return metadata
}

// Deliver reauthorizes the same retained bytes and returns an owned copy for one unary response.
// OUTPUT SEND may be reissued only for the exact retained tuple; INPUT never uses this replay rule.
// A lost buffer, unknown admission, mismatched digest or exhausted budget cannot reexecute a command.
// Deliver заново допускает те же сохранённые bytes и возвращает owned copy для одного unary response.
// OUTPUT SEND перевыдаётся только для exact retained tuple; INPUT не использует это правило replay.
// Потеря buffer, unknown admission, неверный digest или исчерпание бюджета не перезапускают команду.
func (window *OutputWindow) Deliver(ctx context.Context) (value.KubernetesStreamFrame, error) {
	var empty value.KubernetesStreamFrame
	if !window.acquire(ctx) {
		return empty, targetrepo.ErrFrameUnavailable
	}
	defer window.release()
	if ctx.Err() != nil || window.closed || !window.grant.Valid(window.now()) || window.frame.Sequence == 0 || window.acknowledgementPending ||
		window.deliveries >= maximumOutputDeliveries || window.deliveredBytes+uint64(len(window.frame.Payload)) > window.grant.MaximumTotalBytes {
		return empty, targetrepo.ErrFrameUnavailable
	}
	digest := sha256.Sum256(window.frame.Payload)
	if hex.EncodeToString(digest[:]) != window.frame.PayloadDigest {
		return empty, targetrepo.ErrFrameUnavailable
	}
	nativeUntil, hasDeadline := ctx.Deadline()
	if !hasDeadline || !nativeUntil.After(window.now()) {
		return empty, targetrepo.ErrFrameUnavailable
	}
	metadata := window.metadata()
	metadata.NativeAuthorizedUntil = nativeUntil
	window.deliveries++
	work, cancel := context.WithTimeout(ctx, outputOwnerTimeout)
	admission, err := window.owner.BeginKubernetesFrame(work, window.binding, metadata)
	cancel()
	// CLOSED is a durable owner loss marker for an unadmitted native candidate, never a delivery or ACK.
	// Its bytes remain charged; only the provisional sequence can be reused by a sibling candidate.
	// CLOSED — durable owner loss marker для unadmitted native candidate, не delivery либо ACK.
	// Bytes остаются учтёнными; только provisional sequence может использовать sibling candidate.
	if err == nil && ctx.Err() == nil && admission.Disposition == "CLOSED" && metadata.PortForward != nil && !window.delivered && window.deliveryOrdinal == 0 && admission.ExpiresAt.IsZero() && admission.DeliveryOrdinal == 0 {
		clear(window.frame.Payload)
		window.frame = value.KubernetesStreamFrame{}
		window.nextSequence--
		window.deliveries = 0
		return empty, nil
	}
	if err != nil || ctx.Err() != nil || admission.Disposition != "SEND" || !admission.ExpiresAt.After(window.now()) || admission.ExpiresAt.After(window.grant.ExpiresAt) || admission.ExpiresAt.After(nativeUntil) || admission.ExpiresAt.After(window.now().Add(5*time.Second)) ||
		admission.DeliveryOrdinal <= window.deliveryOrdinal || admission.DeliveryOrdinal > maximumOutputDeliveries {
		return empty, targetrepo.ErrFrameUnavailable
	}
	window.delivered = true
	window.deliveryOrdinal = admission.DeliveryOrdinal
	window.deliveredBytes += uint64(len(window.frame.Payload))
	result := window.frame
	result.AuthorizedUntil = admission.ExpiresAt
	result.DeliveryOrdinal = admission.DeliveryOrdinal
	result.Payload = bytes.Clone(window.frame.Payload)
	return result, nil
}

// Acknowledge records only an exact delivered sequence; repeating a lost receipt does not redeliver bytes.
// The private buffer is cleared only after authoritative owner acknowledgement, or explicit Close.
// Acknowledge сохраняет только exact delivered sequence; повтор потерянного receipt не доставляет bytes снова.
// Private buffer очищается только после authoritative owner acknowledgement либо явного Close.
func (window *OutputWindow) Acknowledge(ctx context.Context, sequence uint64) error {
	if !window.acquire(ctx) {
		return targetrepo.ErrFrameUnavailable
	}
	defer window.release()
	if ctx.Err() != nil || window.closed {
		return targetrepo.ErrFrameUnavailable
	}
	if sequence == window.acknowledged {
		return nil
	}
	if window.frame.Sequence == 0 || sequence != window.frame.Sequence || !window.delivered {
		return targetrepo.ErrFrameUnavailable
	}
	window.acknowledgementPending = true
	work, cancel := context.WithTimeout(ctx, outputOwnerTimeout)
	ack, err := window.owner.CompleteKubernetesFrame(work, window.binding, window.metadata())
	cancel()
	if err != nil || ack != sequence {
		return targetrepo.ErrFrameUnavailable
	}
	window.acknowledged = sequence
	clear(window.frame.Payload)
	window.frame = value.KubernetesStreamFrame{}
	window.acknowledgementPending = false
	window.delivered = false
	return nil
}

// Close joins current local receipt work and destroys the only retained output copy without owner success.
// Close ожидает текущее local receipt work и уничтожает единственную retained output copy без owner success.
func (window *OutputWindow) Close() {
	if window == nil {
		return
	}
	<-window.access
	defer window.release()
	window.closed = true
	clear(window.frame.Payload)
	window.frame = value.KubernetesStreamFrame{}
}
