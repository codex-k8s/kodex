package grpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// KubernetesStreams exchanges bounded frames only inside an existing exact owner-bound native stream.
// KubernetesStreams передаёт bounded frames только внутри existing exact owner-bound native stream.
type KubernetesStreams interface {
	// Exchange checks current authority for retained output and one-shot input without reconnecting.
	// Exchange проверяет current authority для retained output и one-shot input без reconnect.
	Exchange(context.Context, managedkubernetes.Binding, string, uint64, *value.KubernetesStreamFrame) (value.KubernetesStreamExchange, error)
}

// WithKubernetesStreams connects the private stream lane without modifying ordinary Secret handlers.
// WithKubernetesStreams подключает private stream lane без изменения ordinary Secret handlers.
func WithKubernetesStreams(streams KubernetesStreams) Option {
	return func(server *Server) { server.kubernetesStreams = streams }
}

// managedKubernetesStreamResponse exposes only exact finite owner credit, never target or private identity material.
// managedKubernetesStreamResponse выдаёт только exact finite owner credit без target либо private identity material.
func managedKubernetesStreamResponse(ctx context.Context, binding managedkubernetes.Binding, result targetrepo.OperationExecution) (*sb.ExecuteManagedKubernetesOperationResponse, error) {
	grant, effect := result.Stream, result.Effect
	if ctx.Err() != nil || grant == nil || !grant.Valid(time.Now()) || grant.Generation != binding.Generation || !managedkubernetes.ValidLocator(effect.PlanRef) || !managedkubernetes.ValidLocator(effect.ChildRef) || !managedkubernetes.ValidLocator(effect.EffectRef) || effect.EffectGeneration < 1 || effect.Result != nil || effect.GateRef != "" || effect.ReconcileOnly || !effect.AuthorizedUntil.IsZero() {
		return nil, status.Error(codes.Unavailable, kubernetesResultUnavailable)
	}
	descriptor := &sb.ManagedKubernetesStreamDescriptor{StreamRef: grant.Ref, InvocationRef: binding.InvocationRef, Generation: grant.Generation, ExpiresAt: timestamppb.New(grant.ExpiresAt), MaximumFrameBytes: grant.MaximumFrameBytes, MaximumTotalBytes: grant.MaximumTotalBytes}
	for _, channel := range grant.InputChannels {
		descriptor.InputChannels = append(descriptor.InputChannels, kubernetesChannelToProto(channel))
	}
	for _, channel := range grant.OutputChannels {
		descriptor.OutputChannels = append(descriptor.OutputChannels, kubernetesChannelToProto(channel))
	}
	return &sb.ExecuteManagedKubernetesOperationResponse{Result: &sb.ExecuteManagedKubernetesOperationResponse_Stream{Stream: descriptor}}, nil
}

// kubernetesInvocationBinding rejects hidden wire fields and connection-test bindings on the frame lane.
// kubernetesInvocationBinding отвергает hidden wire fields и connection-test bindings на frame lane.
func kubernetesInvocationBinding(input *sb.ManagedKubernetesInvocationBinding) (managedkubernetes.Binding, bool) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return managedkubernetes.Binding{}, false
	}
	binding := managedkubernetes.Binding{WorkloadInstance: input.GetWorkloadInstance(), InvocationRef: input.GetInvocationRef(), ConnectionTestRef: input.GetConnectionTestRef(), LeaseRef: input.GetLeaseRef(), Fence: input.GetFence(), Generation: input.GetGeneration(), InputDigest: input.GetInputDigest()}
	return binding, binding.Validate() == nil && binding.InvocationRef != ""
}

// ExchangeManagedKubernetesStream validates exact transport admission before interpreting frame bytes.
// The consumer owns input deduplication and fresh output authority; this adapter cannot reopen a stream.
// ExchangeManagedKubernetesStream проверяет exact transport admission до интерпретации frame bytes.
// Consumer владеет input deduplication и fresh output authority; adapter не может переоткрыть stream.
func (server *Server) ExchangeManagedKubernetesStream(ctx context.Context, request *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
	if err := managedKubernetesAuthority(ctx, sb.ManagedKubernetesService_ExchangeManagedKubernetesStream_FullMethodName, "kubernetes.stream.exchange"); err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(request.GetStreamRef()) {
		return nil, status.Error(codes.InvalidArgument, kubernetesStreamRequestInvalid)
	}
	binding, valid := kubernetesInvocationBinding(request.GetBinding())
	if !valid {
		return nil, status.Error(codes.InvalidArgument, kubernetesBindingInvalid)
	}
	var input *value.KubernetesStreamFrame
	if wire := request.GetInput(); wire != nil {
		channel := kubernetesChannelFromProto(wire.GetChannel())
		digest := sha256.Sum256(wire.GetPayload())
		if len(wire.ProtoReflect().GetUnknown()) != 0 || wire.GetDeliveryOrdinal() != 0 || channel == "" || !managedkubernetes.ValidFrameSequence(wire.GetSequence()) || len(wire.GetPayload()) > managedkubernetes.MaximumFrameBytes || hex.EncodeToString(digest[:]) != wire.GetPayloadDigest() {
			return nil, status.Error(codes.InvalidArgument, "managed Kubernetes frame is invalid")
		}
		input = &value.KubernetesStreamFrame{Sequence: wire.GetSequence(), Channel: channel, Payload: bytes.Clone(wire.GetPayload()), PayloadDigest: wire.GetPayloadDigest(), EndOfInput: wire.GetEndOfInput()}
		defer clear(input.Payload)
	}
	if server.kubernetesStreams == nil {
		return nil, status.Error(codes.Unavailable, kubernetesOperationPathUnavailable)
	}
	result, err := server.kubernetesStreams.Exchange(ctx, binding, request.GetStreamRef(), request.GetAcknowledgedOutputSequence(), input)
	defer func() {
		for index := range result.Output {
			clear(result.Output[index].Payload)
		}
	}()
	if err != nil || ctx.Err() != nil {
		return nil, status.Error(codes.Unavailable, kubernetesOperationPathUnavailable)
	}
	return kubernetesExchangeToProto(result)
}

// kubernetesExchangeToProto refuses unknown statuses and oversized or malformed output without leaking diagnostics.
// kubernetesExchangeToProto отвергает unknown statuses и oversized/malformed output без утечки diagnostics.
func kubernetesExchangeToProto(result value.KubernetesStreamExchange) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
	state, outcome := kubernetesStateToProto(result.State), kubernetesOutcomeToProto(result.RemoteOutcome)
	if state == 0 || outcome == 0 || len(result.Output) > 1 || result.State == "ACTIVE" && (result.RemoteOutcome != "RUNNING" || result.ReasonCode != "") || result.State != "ACTIVE" && (result.RemoteOutcome == "RUNNING" || result.RemoteOutcome == "NOT_STARTED" || result.ReasonCode == "") || result.State == "COMPLETED" && result.RemoteOutcome == "UNKNOWN" || result.ReasonCode != "" && !managedkubernetes.ValidStreamReason(result.ReasonCode) {
		return nil, status.Error(codes.Unavailable, kubernetesResultUnavailable)
	}
	response := &sb.ExchangeManagedKubernetesStreamResponse{AcknowledgedInputSequence: result.AcknowledgedInput, State: state, RemoteOutcome: outcome, ReasonCode: result.ReasonCode}
	for _, frame := range result.Output {
		channel := kubernetesChannelToProto(frame.Channel)
		digest := sha256.Sum256(frame.Payload)
		if channel == 0 || frame.DeliveryOrdinal == 0 || frame.DeliveryOrdinal > managedkubernetes.MaximumOutputDeliveries || !managedkubernetes.ValidFrameSequence(frame.Sequence) || len(frame.Payload) == 0 || len(frame.Payload) > managedkubernetes.MaximumFrameBytes || hex.EncodeToString(digest[:]) != frame.PayloadDigest || frame.EndOfInput || !frame.AuthorizedUntil.After(time.Now()) || frame.AuthorizedUntil.After(time.Now().Add(5*time.Second)) {
			return nil, status.Error(codes.Unavailable, kubernetesResultUnavailable)
		}
		if response.AuthorizedUntil == nil || frame.AuthorizedUntil.Before(response.AuthorizedUntil.AsTime()) {
			response.AuthorizedUntil = timestamppb.New(frame.AuthorizedUntil)
		}
		response.Output = append(response.Output, &sb.ManagedKubernetesFrame{Sequence: frame.Sequence, Channel: channel, Payload: bytes.Clone(frame.Payload), PayloadDigest: frame.PayloadDigest, DeliveryOrdinal: frame.DeliveryOrdinal})
	}
	return response, nil
}

// kubernetesChannelToProto maps only the closed shared channel set and never coerces arbitrary names.
// kubernetesChannelToProto отображает только closed shared channel set без преобразования произвольных имён.
func kubernetesChannelToProto(channel string) sb.ManagedKubernetesChannel {
	switch channel {
	case "CONTROL":
		return sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_CONTROL
	case "STDIN":
		return sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN
	case "STDOUT":
		return sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT
	case "STDERR":
		return sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDERR
	case "TERMINAL_RESIZE":
		return sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_TERMINAL_RESIZE
	case "PORT_FORWARD":
		return sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_PORT_FORWARD
	case "FILE_CONTENT":
		return sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_FILE_CONTENT
	default:
		return sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_UNSPECIFIED
	}
}

// kubernetesChannelFromProto refuses future or unknown enum values before any owner or native I/O.
// kubernetesChannelFromProto отвергает future/unknown enum values до owner либо native I/O.
func kubernetesChannelFromProto(channel sb.ManagedKubernetesChannel) string {
	switch channel {
	case sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_CONTROL:
		return "CONTROL"
	case sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDIN:
		return "STDIN"
	case sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT:
		return "STDOUT"
	case sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDERR:
		return "STDERR"
	case sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_TERMINAL_RESIZE:
		return "TERMINAL_RESIZE"
	case sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_PORT_FORWARD:
		return "PORT_FORWARD"
	case sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_FILE_CONTENT:
		return "FILE_CONTENT"
	default:
		return ""
	}
}

// kubernetesStateToProto maps local lifecycle without treating it as remote outcome.
// kubernetesStateToProto отображает local lifecycle, не считая его remote outcome.
func kubernetesStateToProto(state string) sb.ManagedKubernetesStreamState {
	switch state {
	case "ACTIVE":
		return sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE
	case "COMPLETED":
		return sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED
	case "CANCELED":
		return sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_CANCELED
	case "REVOKED":
		return sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_REVOKED
	case "EXPIRED":
		return sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_EXPIRED
	case "LOST":
		return sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_LOST
	default:
		return sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_UNSPECIFIED
	}
}

// kubernetesOutcomeToProto preserves explicit remote uncertainty without fabricating success.
// kubernetesOutcomeToProto сохраняет explicit remote uncertainty без выдуманного success.
func kubernetesOutcomeToProto(outcome string) sb.ManagedKubernetesRemoteOutcome {
	switch outcome {
	case "NOT_STARTED":
		return sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_NOT_STARTED
	case "RUNNING":
		return sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_RUNNING
	case "SUCCEEDED":
		return sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED
	case "FAILED":
		return sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED
	case "UNKNOWN":
		return sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN
	default:
		return sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNSPECIFIED
	}
}

// KubernetesStreamCancellation requires authoritative closure before local cancellation and joined acknowledgement.
// KubernetesStreamCancellation требует authoritative closure до local cancellation и joined acknowledgement.
type KubernetesStreamCancellation interface {
	// Cancel verifies the original owner closure receipt and never returns private output or dispatch credit.
	// Cancel сверяет original owner closure receipt без выдачи private output либо dispatch credit.
	Cancel(context.Context, managedkubernetes.Binding, string) (value.KubernetesStreamExchange, bool, error)
}

// CancelManagedKubernetesOperation admits only the cancellation method and an exact previously owned stream.
// The domain requires a matching owner closure proof; the stream locator itself carries no cancellation authority.
// CancelManagedKubernetesOperation допускает лишь cancellation method и exact previously owned stream.
// Domain требует matching owner closure proof; stream locator сам не даёт cancellation authority.
func (server *Server) CancelManagedKubernetesOperation(ctx context.Context, request *sb.CancelManagedKubernetesOperationRequest) (*sb.CancelManagedKubernetesOperationResponse, error) {
	if err := managedKubernetesAuthority(ctx, sb.ManagedKubernetesService_CancelManagedKubernetesOperation_FullMethodName, "kubernetes.cancel"); err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(request.GetStreamRef()) {
		return nil, status.Error(codes.InvalidArgument, kubernetesStreamRequestInvalid)
	}
	binding, valid := kubernetesInvocationBinding(request.GetBinding())
	if !valid {
		return nil, status.Error(codes.InvalidArgument, kubernetesBindingInvalid)
	}
	consumer, ok := server.kubernetesStreams.(KubernetesStreamCancellation)
	if !ok {
		return nil, status.Error(codes.Unavailable, kubernetesOperationPathUnavailable)
	}
	result, joined, err := consumer.Cancel(ctx, binding, request.GetStreamRef())
	defer func() {
		for index := range result.Output {
			clear(result.Output[index].Payload)
		}
	}()
	if err != nil || ctx.Err() != nil || len(result.Output) != 0 || result.State == "ACTIVE" || !joined && result.RemoteOutcome != "UNKNOWN" {
		return nil, status.Error(codes.Unavailable, kubernetesOperationPathUnavailable)
	}
	wire, err := kubernetesExchangeToProto(result)
	if err != nil {
		return nil, err
	}
	return &sb.CancelManagedKubernetesOperationResponse{State: wire.State, RemoteOutcome: wire.RemoteOutcome, TransportJoined: joined}, nil
}
