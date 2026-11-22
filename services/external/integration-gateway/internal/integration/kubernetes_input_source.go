package integration

import (
	"bytes"
	"context"
	"slices"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

// KubernetesInputDelivery owns one staged input and its original nonrenewable handoff deadline.
// The runner must clamp Exchange to this deadline and clear Frame.Payload on every exit; broker BeginINPUT remains mandatory.
// KubernetesInputDelivery владеет одним staged input и исходным непродлеваемым сроком передачи.
// Runner ограничивает Exchange этим сроком и очищает Frame.Payload при любом выходе; broker BeginINPUT остаётся обязательным.
type KubernetesInputDelivery struct {
	Frame             *sb.ManagedKubernetesFrame `json:"-"`
	DeliveryExpiresAt time.Time
}

// String excludes private payload and original owner binding from diagnostics.
// String исключает private payload и original owner binding из диагностики.
func (*KubernetesInputDelivery) String() string { return "[managed Kubernetes private input delivery]" }

// GoString preserves redaction under detailed formatting.
// GoString сохраняет сокрытие данных при подробном форматировании.
func (delivery *KubernetesInputDelivery) GoString() string { return delivery.String() }

// KubernetesStreamInputSource claims at most one input per call without inventing EOF from absence.
// An uncertain claim must terminate the runner path; callers must never retry that claim or replay its bytes.
// KubernetesStreamInputSource забирает не более одного input за вызов, не превращая отсутствие в EOF.
// Неопределённый claim завершает runner path; caller не повторяет claim либо его bytes.
type KubernetesStreamInputSource func(context.Context, *sb.ManagedKubernetesInvocationBinding, *sb.ManagedKubernetesStreamDescriptor) (*KubernetesInputDelivery, error)

// NewKubernetesInputSource uses the protected owner client directly; missing composition remains unavailable.
// NewKubernetesInputSource использует защищённый owner client напрямую; отсутствующий client оставляет путь недоступным.
func NewKubernetesInputSource(owner cp.ManagedKubernetesOwnerServiceClient) KubernetesStreamInputSource {
	if owner == nil {
		return nil
	}
	return func(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, descriptor *sb.ManagedKubernetesStreamDescriptor) (*KubernetesInputDelivery, error) {
		return ClaimKubernetesStreamInput(ctx, binding, descriptor, owner)
	}
}

// ClaimKubernetesStreamInput makes one original-binding RPC and transfers only a fully validated owned frame.
// NONE is an empty poll; UNKNOWN/CLOSED and malformed responses are closed errors, with no payload retry or reconciliation read.
// ClaimKubernetesStreamInput выполняет один RPC с original binding и передаёт лишь полностью проверенный owned frame.
// NONE означает пустой poll; UNKNOWN/CLOSED и malformed responses закрывают путь без payload retry либо reconciliation read.
func ClaimKubernetesStreamInput(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, descriptor *sb.ManagedKubernetesStreamDescriptor, owner cp.ManagedKubernetesOwnerServiceClient) (*KubernetesInputDelivery, error) {
	wire, err := kubernetesOwnerBinding(binding)
	if err != nil || ctx == nil || owner == nil || !validKubernetesInputDescriptor(binding, descriptor, time.Now()) {
		return nil, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	// Snapshot the local pins before a client receives a mutable request object.
	// Фиксируем local pins до передачи изменяемого request object клиенту.
	original := managedkubernetes.Binding{WorkloadInstance: wire.WorkloadInstance, InvocationRef: wire.InvocationRef, LeaseRef: wire.LeaseRef, Fence: wire.Fence, Generation: wire.Generation, InputDigest: wire.InputDigest}
	stream := proto.Clone(descriptor).(*sb.ManagedKubernetesStreamDescriptor)
	deadline := time.Now().Add(kubernetesStreamCallTimeout)
	if stream.ExpiresAt.AsTime().Before(deadline) {
		deadline = stream.ExpiresAt.AsTime()
	}
	call, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if call.Err() != nil {
		return nil, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	response, err := owner.ClaimManagedKubernetesInputFrame(call, &cp.ClaimManagedKubernetesInputFrameRequest{Binding: wire, StreamRef: stream.StreamRef})
	if response != nil && response.Frame != nil {
		defer clear(response.Frame.Payload)
	}
	if err != nil || call.Err() != nil {
		return nil, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return nil, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	if response.Disposition == cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_NONE && response.Frame == nil {
		return nil, nil
	}
	if response.Disposition != cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_DELIVER || response.Frame == nil {
		return nil, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	frame := response.Frame
	if len(frame.ProtoReflect().GetUnknown()) != 0 || frame.DeliveryExpiresAt == nil || frame.DeliveryExpiresAt.CheckValid() != nil || len(frame.DeliveryExpiresAt.ProtoReflect().GetUnknown()) != 0 {
		return nil, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	value := managedkubernetes.InputFrameDelivery{Ref: frame.Ref, InvocationRef: frame.InvocationRef, StreamRef: frame.StreamRef, Generation: frame.Generation, Sequence: frame.Sequence, Channel: frame.Channel, PayloadDigest: frame.PayloadDigest, ByteCount: frame.ByteCount, EndOfInput: frame.EndOfInput, Payload: frame.Payload, BindingDigest: frame.BindingDigest, DeliveryExpiresAt: frame.DeliveryExpiresAt.AsTime()}
	channelNumber, ok := sb.ManagedKubernetesChannel_value["MANAGED_KUBERNETES_CHANNEL_"+value.Channel]
	channel := sb.ManagedKubernetesChannel(channelNumber)
	if !ok || !slices.Contains(stream.InputChannels, channel) || value.ByteCount > stream.MaximumFrameBytes || value.DeliveryExpiresAt.After(stream.ExpiresAt.AsTime()) || value.Validate(original, stream.StreamRef, time.Now()) != nil {
		return nil, &SafeError{Code: kubernetesOwnerResponseInvalid}
	}
	return &KubernetesInputDelivery{Frame: &sb.ManagedKubernetesFrame{Sequence: value.Sequence, Channel: channel, Payload: bytes.Clone(value.Payload), PayloadDigest: value.PayloadDigest, EndOfInput: value.EndOfInput}, DeliveryExpiresAt: value.DeliveryExpiresAt}, nil
}

// validKubernetesInputDescriptor checks immutable stream scope and registered directional bounds without granting input authority.
// validKubernetesInputDescriptor проверяет immutable stream scope и directional bounds, не выдавая input authority.
func validKubernetesInputDescriptor(binding *sb.ManagedKubernetesInvocationBinding, stream *sb.ManagedKubernetesStreamDescriptor, now time.Time) bool {
	if binding == nil || stream == nil || len(stream.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(stream.StreamRef) || len(stream.StreamRef) > 96 || stream.InvocationRef != binding.InvocationRef || stream.Generation != binding.Generation || stream.ExpiresAt == nil || stream.ExpiresAt.CheckValid() != nil || len(stream.ExpiresAt.ProtoReflect().GetUnknown()) != 0 || !stream.ExpiresAt.AsTime().After(now) || stream.ExpiresAt.AsTime().After(now.Add(600*time.Second)) || stream.MaximumFrameBytes == 0 || stream.MaximumFrameBytes > managedkubernetes.MaximumFrameBytes || stream.MaximumTotalBytes < uint64(stream.MaximumFrameBytes) || stream.MaximumTotalBytes > managedkubernetes.MaximumStreamBytes || len(stream.InputChannels) == 0 || len(stream.OutputChannels) == 0 {
		return false
	}
	for direction, channels := range [][]sb.ManagedKubernetesChannel{stream.InputChannels, stream.OutputChannels} {
		if len(channels) > 7 {
			return false
		}
		for i, channel := range channels {
			if slices.Contains(channels[:i], channel) || !validKubernetesStreamChannel(channel, direction == 0) {
				return false
			}
		}
	}
	return true
}
