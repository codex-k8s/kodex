package controlplane

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// kubernetesFrameDirection rejects malformed metadata before crossing the protected owner boundary.
// kubernetesFrameDirection отклоняет неверные metadata до protected owner boundary.
func kubernetesFrameDirection(frame value.KubernetesFrameMetadata) (cp.ManagedKubernetesFrameDirection, bool) {
	if !managedkubernetes.ValidLocator(frame.StreamRef) || !managedkubernetes.ValidFrameSequence(frame.Sequence) ||
		!managedkubernetes.ValidChannel(frame.Channel) || !managedkubernetes.ValidDigest(frame.PayloadDigest) ||
		frame.ByteCount > managedkubernetes.MaximumFrameBytes {
		return cp.ManagedKubernetesFrameDirection_MANAGED_KUBERNETES_FRAME_DIRECTION_UNSPECIFIED, false
	}
	if frame.PortForward != nil {
		if frame.Direction != "OUTPUT" || frame.Channel != "PORT_FORWARD" || frame.EndOfInput || frame.PortForward.ValidateOutput(frame.ByteCount, managedkubernetes.MaximumForwardPorts) != nil {
			return cp.ManagedKubernetesFrameDirection_MANAGED_KUBERNETES_FRAME_DIRECTION_UNSPECIFIED, false
		}
	} else if frame.Direction == "OUTPUT" && frame.Channel == "PORT_FORWARD" {
		return cp.ManagedKubernetesFrameDirection_MANAGED_KUBERNETES_FRAME_DIRECTION_UNSPECIFIED, false
	}
	switch frame.Direction {
	case "INPUT":
		return cp.ManagedKubernetesFrameDirection_MANAGED_KUBERNETES_FRAME_DIRECTION_INPUT, true
	case "OUTPUT":
		return cp.ManagedKubernetesFrameDirection_MANAGED_KUBERNETES_FRAME_DIRECTION_OUTPUT, true
	default:
		return cp.ManagedKubernetesFrameDirection_MANAGED_KUBERNETES_FRAME_DIRECTION_UNSPECIFIED, false
	}
}

// BeginKubernetesFrame forwards exact gateway lineage and payload-free intent through the authenticated client.
// Unknown or malformed owner replies cannot authorize transport and are never retried here.
// BeginKubernetesFrame передаёт exact gateway lineage и intent без payload через authenticated client.
// Неизвестные либо неверные owner replies не разрешают transport и здесь не повторяются.
func (owner *Owner) BeginKubernetesFrame(ctx context.Context, binding managedkubernetes.Binding, frame value.KubernetesFrameMetadata) (value.KubernetesFrameAdmission, error) {
	var result value.KubernetesFrameAdmission
	direction, valid := kubernetesFrameDirection(frame)
	if owner == nil || owner.client == nil || owner.client.KubernetesOperations == nil || !valid ||
		binding.Validate() != nil || binding.InvocationRef == "" {
		return result, targetrepo.ErrFrameUnavailable
	}
	var nativeUntil *timestamppb.Timestamp
	if frame.Direction == "OUTPUT" {
		if !frame.NativeAuthorizedUntil.After(time.Now()) {
			return result, targetrepo.ErrFrameUnavailable
		}
		nativeUntil = timestamppb.New(frame.NativeAuthorizedUntil)
		if nativeUntil.CheckValid() != nil {
			return result, targetrepo.ErrFrameUnavailable
		}
	} else if !frame.NativeAuthorizedUntil.IsZero() {
		return result, targetrepo.ErrFrameUnavailable
	}
	var forward *cp.ManagedKubernetesPortForwardFrameBinding
	if frame.PortForward != nil {
		witness := frame.PortForward
		forward = &cp.ManagedKubernetesPortForwardFrameBinding{ConnectionRef: witness.ConnectionRef, Generation: int64(witness.Generation), Slot: uint32(witness.Slot), Action: cp.ManagedKubernetesPortForwardFrameAction(witness.Action), NativeOrdinal: witness.NativeOrdinal}
	}
	response, err := owner.client.KubernetesOperations.BeginManagedKubernetesFrame(ctx, &cp.BeginManagedKubernetesFrameRequest{
		Binding: kubernetesHealthBindingToProto(binding), StreamRef: frame.StreamRef, Direction: direction,
		Sequence: frame.Sequence, Channel: frame.Channel, ByteCount: frame.ByteCount, PayloadDigest: frame.PayloadDigest, EndOfInput: frame.EndOfInput, NativeAuthorizedUntil: nativeUntil, PortForwardConnection: forward,
	})
	if err != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return result, targetrepo.ErrFrameUnavailable
	}
	if len(response.PortForwardRetention) > 0 {
		if frame.Direction != "INPUT" || frame.Channel != "PORT_FORWARD" || response.Disposition != cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_SEND || len(response.PortForwardRetention) > managedkubernetes.MaximumPortForwardConnections {
			return result, targetrepo.ErrFrameUnavailable
		}
		set := &managedkubernetes.PortForwardRetentionSet{}
		for _, item := range response.PortForwardRetention {
			if item == nil || len(item.ProtoReflect().GetUnknown()) != 0 || item.Generation < 1 || item.Slot >= managedkubernetes.MaximumForwardPorts {
				return result, targetrepo.ErrFrameUnavailable
			}
			set.Connections = append(set.Connections, managedkubernetes.PortForwardConnectionIdentity{ConnectionRef: item.ConnectionRef, Generation: uint64(item.Generation), Slot: byte(item.Slot)})
		}
		if set.Validate(managedkubernetes.MaximumForwardPorts, nil) != nil {
			return result, targetrepo.ErrFrameUnavailable
		}
		result.PortForwardRetention = set
	}
	switch response.GetDisposition() {
	case cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_SEND:
		result.Disposition = "SEND"
		if response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil || len(response.GetExpiresAt().ProtoReflect().GetUnknown()) != 0 {
			return value.KubernetesFrameAdmission{}, targetrepo.ErrFrameUnavailable
		}
		result.ExpiresAt = response.GetExpiresAt().AsTime()
		result.DeliveryOrdinal = response.DeliveryOrdinal
		if frame.Direction == "OUTPUT" {
			now := time.Now()
			if result.DeliveryOrdinal < 1 || result.DeliveryOrdinal > managedkubernetes.MaximumOutputDeliveries || !result.ExpiresAt.After(now) || result.ExpiresAt.After(now.Add(5*time.Second)) || result.ExpiresAt.After(frame.NativeAuthorizedUntil) {
				return value.KubernetesFrameAdmission{}, targetrepo.ErrFrameUnavailable
			}
		} else if result.DeliveryOrdinal != 0 {
			return value.KubernetesFrameAdmission{}, targetrepo.ErrFrameUnavailable
		}
	case cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_ACKNOWLEDGED:
		result.Disposition = "ACKNOWLEDGED"
	case cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_UNKNOWN:
		result.Disposition = "UNKNOWN"
	case cp.ManagedKubernetesFrameDisposition_MANAGED_KUBERNETES_FRAME_DISPOSITION_CLOSED:
		if frame.Direction != "OUTPUT" || frame.Channel != "PORT_FORWARD" || frame.PortForward == nil {
			return result, targetrepo.ErrFrameUnavailable
		}
		result.Disposition = "CLOSED"
	default:
		return result, targetrepo.ErrFrameUnavailable
	}
	if result.Disposition != "SEND" && (response.ExpiresAt != nil || response.DeliveryOrdinal != 0) {
		return value.KubernetesFrameAdmission{}, targetrepo.ErrFrameUnavailable
	}
	return result, nil
}

// CompleteKubernetesFrame publishes one joined-write receipt without sending bytes or replaying the write.
// A lost reply or a different sequence preserves uncertainty for the stream's terminal owner transition.
// CompleteKubernetesFrame публикует один joined-write receipt без bytes либо повтора записи.
// Потерянный ответ или иной sequence сохраняет uncertainty для terminal owner transition потока.
func (owner *Owner) CompleteKubernetesFrame(ctx context.Context, binding managedkubernetes.Binding, frame value.KubernetesFrameMetadata) (uint64, error) {
	direction, valid := kubernetesFrameDirection(frame)
	if owner == nil || owner.client == nil || owner.client.KubernetesOperations == nil || !valid ||
		binding.Validate() != nil || binding.InvocationRef == "" {
		return 0, targetrepo.ErrFrameUnavailable
	}
	response, err := owner.client.KubernetesOperations.CompleteManagedKubernetesFrame(ctx, &cp.CompleteManagedKubernetesFrameRequest{
		Binding: kubernetesHealthBindingToProto(binding), StreamRef: frame.StreamRef, Direction: direction,
		Sequence: frame.Sequence, PayloadDigest: frame.PayloadDigest,
	})
	if err != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.GetAcknowledgedSequence() != frame.Sequence {
		return 0, targetrepo.ErrFrameUnavailable
	}
	return response.GetAcknowledgedSequence(), nil
}
