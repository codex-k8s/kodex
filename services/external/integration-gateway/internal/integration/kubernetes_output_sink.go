package integration

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// NewKubernetesOutputSink binds protected output acceptance to the existing owner client; a missing client leaves composition unavailable.
// NewKubernetesOutputSink связывает защищённое принятие output с existing owner client; без client composition остаётся недоступной.
func NewKubernetesOutputSink(owner cp.ManagedKubernetesOwnerServiceClient) KubernetesStreamOutputSink {
	if owner == nil {
		return nil
	}
	return func(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, descriptor *sb.ManagedKubernetesStreamDescriptor, output KubernetesStreamOutput) error {
		return AcceptKubernetesStreamOutput(ctx, binding, descriptor, output, owner)
	}
}

// AcceptKubernetesStreamOutput transfers one owned payload exactly once to protected staging, then requires an exact current ACCEPTED receipt before allowing a broker ACK.
// Lost RPC acknowledgements permit only one metadata read under the original deadline. Acceptance is neither publication nor invocation success.
// AcceptKubernetesStreamOutput передаёт owned payload ровно один раз в защищённое staging и требует exact current ACCEPTED receipt перед broker ACK.
// При потерянном RPC acknowledgement разрешён лишь один metadata read в исходный срок. Принятие не означает publication либо invocation success.
func AcceptKubernetesStreamOutput(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, descriptor *sb.ManagedKubernetesStreamDescriptor, output KubernetesStreamOutput, owner cp.ManagedKubernetesOwnerServiceClient) error {
	defer clear(output.Payload)
	wire, err := kubernetesOwnerBinding(binding)
	if err != nil || ctx == nil || owner == nil || !validKubernetesOutputCandidate(binding, descriptor, output, time.Now()) {
		return &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	call, cancel := context.WithDeadline(ctx, output.AuthorizedUntil)
	defer cancel()
	if call.Err() != nil {
		return &SafeError{Code: kubernetesOwnerUnavailable}
	}
	payload := bytes.Clone(output.Payload)
	defer clear(payload)
	request := &cp.AcceptManagedKubernetesOutputFrameRequest{
		Binding: proto.Clone(wire).(*cp.ManagedKubernetesOwnerBinding), StreamRef: output.StreamRef,
		Sequence: output.Sequence, Channel: kubernetesOutputChannel(output.Channel), ByteCount: uint32(len(payload)),
		PayloadDigest: output.PayloadDigest, DeliveryOrdinal: output.DeliveryOrdinal, Payload: payload,
	}
	response, err := owner.AcceptManagedKubernetesOutputFrame(call, request)
	if err != nil {
		if call.Err() != nil || !uncertainKubernetesOutputAcceptance(err) || !output.AuthorizedUntil.After(time.Now()) {
			return &SafeError{Code: kubernetesOwnerUnavailable}
		}
		// Reconciliation never repeats payload transmission or extends the original ordinal grant.
		// Reconciliation не повторяет передачу payload и не продлевает исходный ordinal grant.
		read, readErr := owner.GetManagedKubernetesOutputFrame(call, &cp.GetManagedKubernetesOutputFrameRequest{
			Binding: proto.Clone(wire).(*cp.ManagedKubernetesOwnerBinding), StreamRef: output.StreamRef, Sequence: output.Sequence, DeliveryOrdinal: output.DeliveryOrdinal,
		})
		if readErr != nil || call.Err() != nil {
			return &SafeError{Code: kubernetesOwnerUnavailable}
		}
		if read == nil || len(read.ProtoReflect().GetUnknown()) != 0 || !validKubernetesOutputReceipt(read.GetReceipt(), output) {
			return &SafeError{Code: kubernetesOwnerResponseInvalid}
		}
		return nil
	}
	if call.Err() != nil {
		return &SafeError{Code: kubernetesOwnerUnavailable}
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validKubernetesOutputReceipt(response.GetReceipt(), output) {
		return &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	return nil
}

// validKubernetesOutputCandidate checks original coordinates and bounded output bytes without treating them as owner authority.
// validKubernetesOutputCandidate проверяет исходные координаты и ограниченные output bytes, не считая их owner authority.
func validKubernetesOutputCandidate(binding *sb.ManagedKubernetesInvocationBinding, descriptor *sb.ManagedKubernetesStreamDescriptor, output KubernetesStreamOutput, now time.Time) bool {
	if binding == nil || descriptor == nil || len(descriptor.ProtoReflect().GetUnknown()) != 0 ||
		!managedkubernetes.ValidLocator(descriptor.StreamRef) || len(descriptor.StreamRef) > 96 || descriptor.InvocationRef != binding.InvocationRef || descriptor.Generation != binding.Generation ||
		descriptor.ExpiresAt == nil || descriptor.ExpiresAt.CheckValid() != nil || len(descriptor.ExpiresAt.ProtoReflect().GetUnknown()) != 0 ||
		!descriptor.ExpiresAt.AsTime().After(now) || descriptor.ExpiresAt.AsTime().After(now.Add(600*time.Second)) ||
		descriptor.MaximumFrameBytes == 0 || descriptor.MaximumFrameBytes > managedkubernetes.MaximumFrameBytes ||
		descriptor.MaximumTotalBytes < uint64(descriptor.MaximumFrameBytes) || descriptor.MaximumTotalBytes > managedkubernetes.MaximumStreamBytes ||
		len(descriptor.OutputChannels) == 0 || output.InvocationRef != binding.InvocationRef || output.Generation != binding.Generation || output.StreamRef != descriptor.StreamRef ||
		len(output.Payload) == 0 || output.DeliveryOrdinal == 0 || output.DeliveryOrdinal > managedkubernetes.MaximumOutputDeliveries ||
		!output.AuthorizedUntil.After(now) || output.AuthorizedUntil.After(now.Add(5*time.Second)) || output.AuthorizedUntil.After(descriptor.ExpiresAt.AsTime()) {
		return false
	}
	for direction, channels := range [][]sb.ManagedKubernetesChannel{descriptor.InputChannels, descriptor.OutputChannels} {
		if len(channels) > 7 {
			return false
		}
		for i, channel := range channels {
			if slices.Contains(channels[:i], channel) || !validKubernetesStreamChannel(channel, direction == 0) {
				return false
			}
		}
	}
	digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest})
	return err == nil && digest == output.BindingDigest && slices.Contains(descriptor.OutputChannels, output.Channel) &&
		validKubernetesStreamPayload(&sb.ManagedKubernetesFrame{Sequence: output.Sequence, Channel: output.Channel, Payload: output.Payload, PayloadDigest: output.PayloadDigest}, descriptor.MaximumFrameBytes)
}

// validKubernetesOutputReceipt requires immutable full-tuple acceptance and exactly the original credit deadline, which metadata reads cannot renew.
// validKubernetesOutputReceipt требует immutable full-tuple acceptance и ровно исходный credit deadline, который metadata reads не продлевают.
func validKubernetesOutputReceipt(receipt *cp.ManagedKubernetesOutputFrameReceipt, output KubernetesStreamOutput) bool {
	return receipt != nil && len(receipt.ProtoReflect().GetUnknown()) == 0 && strings.HasPrefix(receipt.Ref, "kof_") && managedkubernetes.ValidLocator(receipt.Ref) && len(receipt.Ref) <= 96 &&
		receipt.InvocationRef == output.InvocationRef && receipt.StreamRef == output.StreamRef && receipt.Generation == output.Generation &&
		receipt.Sequence == output.Sequence && receipt.Channel == kubernetesOutputChannel(output.Channel) && receipt.PayloadDigest == output.PayloadDigest &&
		receipt.ByteCount == uint32(len(output.Payload)) && receipt.DeliveryOrdinal == output.DeliveryOrdinal && receipt.BindingDigest == output.BindingDigest &&
		receipt.State == cp.ManagedKubernetesOutputFrameState_MANAGED_KUBERNETES_OUTPUT_FRAME_STATE_ACCEPTED &&
		receipt.AuthorizedUntil != nil && receipt.AuthorizedUntil.CheckValid() == nil && len(receipt.AuthorizedUntil.ProtoReflect().GetUnknown()) == 0 &&
		receipt.AuthorizedUntil.AsTime().Equal(output.AuthorizedUntil) && output.AuthorizedUntil.After(time.Now())
}

// kubernetesOutputChannel maps only already validated registered channels to the owner's uppercase wire vocabulary.
// kubernetesOutputChannel переводит только ранее проверенные registered channels в uppercase wire vocabulary владельца.
func kubernetesOutputChannel(channel sb.ManagedKubernetesChannel) string {
	return strings.TrimPrefix(channel.String(), "MANAGED_KUBERNETES_CHANNEL_")
}

// uncertainKubernetesOutputAcceptance allows metadata reconciliation only for transport or server uncertainty, never a definitive denial.
// uncertainKubernetesOutputAcceptance разрешает metadata reconciliation лишь при transport либо server uncertainty, не после однозначного отказа.
func uncertainKubernetesOutputAcceptance(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.Unknown, codes.Internal, codes.DataLoss, codes.DeadlineExceeded, codes.Canceled:
		return true
	default:
		return false
	}
}
