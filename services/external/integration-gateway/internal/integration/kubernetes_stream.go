package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

const (
	// kubernetesStreamCallTimeout bounds a single exchange or joined cleanup; it does not renew the owner lease.
	// kubernetesStreamCallTimeout ограничивает один exchange либо joined cleanup, не продлевая owner lease.
	kubernetesStreamCallTimeout = 10 * time.Second
)

// KubernetesStreamOutput transfers exclusive ownership of one validated private payload to the sink.
// Its coordinates identify the original stream; they confer no read or artifact authority.
// KubernetesStreamOutput передаёт sink исключительное владение одним проверенным private payload.
// Координаты обозначают original stream, не выдавая read либо artifact authority.
type KubernetesStreamOutput struct {
	InvocationRef, StreamRef, BindingDigest string
	Generation                              int64
	Sequence                                uint64
	Channel                                 sb.ManagedKubernetesChannel
	PayloadDigest                           string
	// DeliveryOrdinal is the broker-forwarded durable owner OUTPUT credit, never a local retry counter.
	// DeliveryOrdinal — переданный broker durable owner OUTPUT credit, не local retry counter.
	DeliveryOrdinal uint32
	Payload         []byte `json:"-"`
	AuthorizedUntil time.Time
}

// String excludes output bytes from ordinary diagnostics.
// String исключает output bytes из обычной диагностики.
func (KubernetesStreamOutput) String() string {
	return "[managed Kubernetes accepted-output candidate]"
}

// GoString preserves the private output boundary under detailed formatting.
// GoString сохраняет private output boundary при подробном форматировании.
func (output KubernetesStreamOutput) GoString() string { return output.String() }

// KubernetesStreamOutputSink accepts bytes only inside its protected original-owner delivery scope.
// A nil error means synchronously accepted stdout/artifact delivery before AuthorizedUntil, never queued, counted or discarded bytes.
// It owns Payload immediately, including on error, must prevent acceptance/persistence after its deadline, and clear rejected bytes; detached async acceptance is forbidden.
// KubernetesStreamOutputSink принимает bytes только внутри защищённой original-owner delivery scope.
// Nil error означает synchronous stdout/artifact до AuthorizedUntil, не очередь, подсчёт либо отбрасывание bytes.
// Payload принадлежит sink сразу, включая ошибку; позднее async acceptance/persistence запрещено, rejected bytes очищаются.
type KubernetesStreamOutputSink func(context.Context, *sb.ManagedKubernetesInvocationBinding, *sb.ManagedKubernetesStreamDescriptor, KubernetesStreamOutput) error

// KubernetesStreamClosure confirms authoritative closure of this exact original owner binding.
// Elapsed time alone is not closure proof; the callback neither retries input nor grants new effects.
// KubernetesStreamClosure подтверждает authoritative closure exact original owner binding.
// Истечение времени само по себе не proof; callback не повторяет input и не выдаёт effects.
type KubernetesStreamClosure func(context.Context, *sb.ManagedKubernetesInvocationBinding, string) error

// KubernetesStreamProgress reports protocol facts after accepted delivery; it is not an invocation result.
// A terminal remote exit does not prove remote process quiescence or local broker transport join.
// KubernetesStreamProgress сообщает protocol facts после accepted delivery, не invocation result.
// Terminal remote exit не доказывает remote process quiescence либо local broker transport join.
type KubernetesStreamProgress struct {
	State                             sb.ManagedKubernetesStreamState
	RemoteOutcome                     sb.ManagedKubernetesRemoteOutcome
	ReasonCode                        string
	AcknowledgedInput, AcceptedOutput uint64
}

// KubernetesStreamCleanup preserves distinct local transport join and remote outcome facts.
// A cleanup reply never authorizes output or converts an uncertain exchange into successful delivery.
// KubernetesStreamCleanup сохраняет раздельно local transport join и remote outcome facts.
// Cleanup reply не разрешает output и не превращает uncertain exchange в successful delivery.
type KubernetesStreamCleanup struct {
	State           sb.ManagedKubernetesStreamState
	RemoteOutcome   sb.ManagedKubernetesRemoteOutcome
	TransportJoined bool
}

// KubernetesStreamConsumer serializes one already-started descriptor without opening or replaying effects.
// It owns no goroutines, renewal engine, durable output storage or independently assigned authority.
// KubernetesStreamConsumer сериализует already-started descriptor без открытия либо replay effects.
// Не владеет goroutines, renewal engine, durable output storage либо назначаемыми полномочиями.
type KubernetesStreamConsumer struct {
	binding                                       *sb.ManagedKubernetesInvocationBinding
	descriptor                                    *sb.ManagedKubernetesStreamDescriptor
	bindingDigest, risk                           string
	broker                                        sb.ManagedKubernetesServiceClient
	sink                                          KubernetesStreamOutputSink
	confirmClosure                                KubernetesStreamClosure
	lane                                          chan struct{}
	control                                       sync.Mutex
	stopped                                       bool
	inflight                                      context.CancelFunc
	uncertain, terminal                           bool
	acknowledgedInput, acceptedOutput, totalBytes uint64
	acceptedUntil                                 time.Time
	inputClosed                                   map[sb.ManagedKubernetesChannel]bool
}

// NewKubernetesStreamConsumer snapshots original binding and immutable descriptor; absent output sink or closure callback fails before I/O.
// The caller must obtain the descriptor through ExecuteKubernetes and still own the invocation lifecycle.
// NewKubernetesStreamConsumer фиксирует original binding и immutable descriptor; отсутствие sink либо closure callback закрыто отклоняется до I/O.
// Caller получает descriptor через ExecuteKubernetes и продолжает владеть invocation lifecycle.
func NewKubernetesStreamConsumer(binding *sb.ManagedKubernetesInvocationBinding, descriptor *sb.ManagedKubernetesStreamDescriptor, operation string, broker sb.ManagedKubernetesServiceClient, sink KubernetesStreamOutputSink, confirmClosure KubernetesStreamClosure) (*KubernetesStreamConsumer, error) {
	if _, err := kubernetesOwnerBinding(binding); err != nil || broker == nil || sink == nil || confirmClosure == nil || !validKubernetesStreamDescriptor(binding, descriptor, operation, time.Now()) {
		return nil, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	original := managedkubernetes.Binding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest}
	digest, err := managedkubernetes.BindingDigest(original)
	if err != nil {
		return nil, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	registered, _ := integrationpackage.KubernetesOperationByKey(operation)
	consumer := &KubernetesStreamConsumer{binding: proto.Clone(binding).(*sb.ManagedKubernetesInvocationBinding), descriptor: proto.Clone(descriptor).(*sb.ManagedKubernetesStreamDescriptor), bindingDigest: digest, risk: string(registered.Risk), broker: broker, sink: sink, confirmClosure: confirmClosure, lane: make(chan struct{}, 1), inputClosed: make(map[sb.ManagedKubernetesChannel]bool)}
	consumer.lane <- struct{}{}
	return consumer, nil
}

// String redacts the original fence and any private client state.
// String скрывает original fence и private client state.
func (*KubernetesStreamConsumer) String() string { return "[managed Kubernetes stream consumer]" }

// GoString preserves binding redaction under detailed formatting.
// GoString сохраняет binding redaction при подробном форматировании.
func (consumer *KubernetesStreamConsumer) GoString() string { return consumer.String() }

// validKubernetesStreamDescriptor refuses foreign, future, unknown and directionally invalid grants.
// It does not derive current authority from the descriptor or widen a registered read operation.
// validKubernetesStreamDescriptor отклоняет foreign/future/unknown grants и неверные направления channels.
// Не выводит current authority из descriptor и не расширяет registered READ operation.
func validKubernetesStreamDescriptor(binding *sb.ManagedKubernetesInvocationBinding, stream *sb.ManagedKubernetesStreamDescriptor, operation string, now time.Time) bool {
	registered, ok := integrationpackage.KubernetesOperationByKey(operation)
	if !ok || registered.Execution != integrationpackage.KubernetesStream || binding == nil || stream == nil || len(stream.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(stream.StreamRef) || len(stream.StreamRef) > 96 || stream.InvocationRef != binding.InvocationRef || stream.Generation != binding.Generation || stream.ExpiresAt == nil || stream.ExpiresAt.CheckValid() != nil || len(stream.ExpiresAt.ProtoReflect().GetUnknown()) != 0 || !stream.ExpiresAt.AsTime().After(now) || stream.ExpiresAt.AsTime().After(now.Add(600*time.Second)) || stream.MaximumFrameBytes == 0 || stream.MaximumFrameBytes > managedkubernetes.MaximumFrameBytes || stream.MaximumTotalBytes < uint64(stream.MaximumFrameBytes) || stream.MaximumTotalBytes > managedkubernetes.MaximumStreamBytes || len(stream.OutputChannels) == 0 {
		return false
	}
	for direction, channels := range [][]sb.ManagedKubernetesChannel{stream.InputChannels, stream.OutputChannels} {
		if len(channels) > 7 {
			return false
		}
		for i, channel := range channels {
			if slices.Contains(channels[:i], channel) || !validKubernetesStreamChannel(channel, direction == 0) || direction == 0 && registered.Risk == "READ" && channel != sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_CONTROL {
				return false
			}
		}
	}
	return true
}

// validKubernetesStreamChannel rejects unknown enums and channels on the wrong side of the transport.
// validKubernetesStreamChannel отклоняет unknown enums и channels на неверной стороне transport.
func validKubernetesStreamChannel(channel sb.ManagedKubernetesChannel, input bool) bool {
	switch channel {
	case sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_CONTROL, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_PORT_FORWARD, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_FILE_CONTENT:
		return true
	case sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_TERMINAL_RESIZE:
		return input
	case sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT, sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDERR:
		return !input
	default:
		return false
	}
}

// validKubernetesStreamPayload binds exact bounded bytes without interpreting terminal text as a receipt.
// validKubernetesStreamPayload связывает exact bounded bytes, не считая terminal text квитанцией.
func validKubernetesStreamPayload(frame *sb.ManagedKubernetesFrame, maximum uint32) bool {
	if frame == nil || len(frame.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidFrameSequence(frame.Sequence) || uint64(len(frame.Payload)) > uint64(maximum) || !managedkubernetes.ValidDigest(frame.PayloadDigest) {
		return false
	}
	digest := sha256.Sum256(frame.Payload)
	return hex.EncodeToString(digest[:]) == frame.PayloadDigest
}

// validKubernetesStreamState keeps local lifecycle separate from confirmed remote outcome.
// validKubernetesStreamState сохраняет local lifecycle отдельно от confirmed remote outcome.
func validKubernetesStreamState(state sb.ManagedKubernetesStreamState, outcome sb.ManagedKubernetesRemoteOutcome, reason string) bool {
	switch state {
	case sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE:
		return outcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_RUNNING && reason == ""
	case sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED:
		return outcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED && (reason == "REMOTE_EXIT" || reason == "CLEAN_EOF") ||
			outcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED && (reason == "REMOTE_EXIT" || reason == "REMOTE_FAILURE")
	case sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_CANCELED, sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_REVOKED, sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_EXPIRED, sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_LOST:
		return (outcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED || outcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED || outcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN) && managedkubernetes.ValidStreamReason(reason)
	default:
		return false
	}
}
