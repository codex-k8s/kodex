package integration

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

// TestKubernetesStreamCancelRequiresOwnerClosure preserves independent cleanup and original binding after caller cancellation.
// TestKubernetesStreamCancelRequiresOwnerClosure сохраняет independent cleanup и original binding после caller cancellation.
func TestKubernetesStreamCancelRequiresOwnerClosure(t *testing.T) {
	for _, variant := range []string{"closed", "unavailable", "unjoined-unknown", "unjoined-success", "active", "unknown-wire"} {
		t.Run(variant, func(t *testing.T) {
			binding, descriptor := gatewayStreamPins()
			original := proto.Clone(binding)
			broker := &gatewayStreamBrokerFixture{}
			var closed bool
			broker.cancel = func(ctx context.Context, request *sb.CancelManagedKubernetesOperationRequest) (*sb.CancelManagedKubernetesOperationResponse, error) {
				if !closed || ctx.Err() != nil || !proto.Equal(request.Binding, original) || request.StreamRef != descriptor.StreamRef {
					t.Fatal("cancel preceded owner closure or changed original pins")
				}
				response := &sb.CancelManagedKubernetesOperationResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_CANCELED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN, TransportJoined: true}
				switch variant {
				case "unjoined-unknown":
					response.TransportJoined = false
				case "unjoined-success":
					response.TransportJoined = false
					response.RemoteOutcome = sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED
				case "active":
					response.State = sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE
				case "unknown-wire":
					response.ProtoReflect().SetUnknown([]byte{0x78, 1})
				}
				return response, nil
			}
			closure := func(ctx context.Context, got *sb.ManagedKubernetesInvocationBinding, ref string) error {
				if ctx.Err() != nil || !proto.Equal(got, original) || ref != descriptor.StreamRef {
					t.Fatal("closure callback lost current bounded context or pins")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("cleanup is unbounded")
				}
				if variant == "unavailable" {
					return errors.New("PRIVATE_OWNER_ERROR")
				}
				closed = true
				got.Fence = "caller mutation"
				return nil
			}
			consumer, err := NewKubernetesStreamConsumer(binding, descriptor, "kubernetes.exec", broker, func(_ context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, out KubernetesStreamOutput) error {
				clear(out.Payload)
				return nil
			}, closure)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer stop()
			result, err := consumer.Cancel(cleanup)
			valid := variant == "closed" || variant == "unjoined-unknown"
			if (err == nil) != valid {
				t.Fatalf("cleanup result validity mismatch: %v", err)
			}
			if variant == "unavailable" && broker.cancels.Load() != 0 {
				t.Fatal("unconfirmed owner closure reached broker")
			}
			if valid && result.RemoteOutcome != sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN {
				t.Fatal("join fabricated remote success")
			}
			if _, err := consumer.Exchange(t.Context(), nil); err == nil || broker.exchanges.Load() != 0 {
				t.Fatal("cleanup reopened exchanges")
			}
		})
	}
}

// TestKubernetesStreamCleanupPreservesCallerBudget prevents an expired cleanup scope or a slow owner callback from receiving a fresh broker budget; local admission remains closed on either failure.
// TestKubernetesStreamCleanupPreservesCallerBudget запрещает новый broker budget после истечения cleanup scope либо медленного owner callback; local admission остаётся закрытым при обоих отказах.
func TestKubernetesStreamCleanupPreservesCallerBudget(t *testing.T) {
	for _, variant := range []string{"expired", "owner-consumed-budget"} {
		t.Run(variant, func(t *testing.T) {
			binding, descriptor := gatewayStreamPins()
			broker := &gatewayStreamBrokerFixture{}
			closureCalls := 0
			cleanup, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			deadline, _ := cleanup.Deadline()
			closure := func(ctx context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ string) error {
				closureCalls++
				actual, ok := ctx.Deadline()
				if !ok || actual.After(deadline) {
					t.Fatal("cleanup reset the remaining owner budget")
				}
				<-ctx.Done()
				return nil
			}
			consumer, err := NewKubernetesStreamConsumer(binding, descriptor, "kubernetes.exec", broker, func(_ context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, out KubernetesStreamOutput) error {
				clear(out.Payload)
				return nil
			}, closure)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "expired" {
				cancel()
			}
			result, err := consumer.Cancel(cleanup)
			if err == nil || result.TransportJoined || broker.cancels.Load() != 0 || variant == "owner-consumed-budget" && closureCalls != 1 || variant == "expired" && closureCalls != 0 {
				t.Fatal("expired cleanup declared join or reached the broker")
			}
			if _, err := consumer.Exchange(t.Context(), nil); err == nil || broker.exchanges.Load() != 0 {
				t.Fatal("failed cleanup reopened stream admission")
			}
		})
	}
}

// TestKubernetesStreamCancelJoinsInflightExchange proves cleanup waits for the actual canceled RPC rather than detached fake join.
// TestKubernetesStreamCancelJoinsInflightExchange доказывает ожидание реального canceled RPC вместо detached fake join.
func TestKubernetesStreamCancelJoinsInflightExchange(t *testing.T) {
	started := make(chan struct{})
	returned := make(chan struct{})
	broker := &gatewayStreamBrokerFixture{}
	var rpcReturned atomic.Bool
	broker.exchange = func(ctx context.Context, _ *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
		close(started)
		<-ctx.Done()
		rpcReturned.Store(true)
		return nil, ctx.Err()
	}
	binding, descriptor := gatewayStreamPins()
	closure := func(context.Context, *sb.ManagedKubernetesInvocationBinding, string) error {
		if !rpcReturned.Load() {
			t.Error("owner closure raced unjoined local RPC")
		}
		return nil
	}
	broker.cancel = func(context.Context, *sb.CancelManagedKubernetesOperationRequest) (*sb.CancelManagedKubernetesOperationResponse, error) {
		if !rpcReturned.Load() {
			t.Error("cleanup bypassed local join")
		}
		return &sb.CancelManagedKubernetesOperationResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_CANCELED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN, TransportJoined: true}, nil
	}
	consumer, err := NewKubernetesStreamConsumer(binding, descriptor, "kubernetes.exec", broker, func(_ context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, out KubernetesStreamOutput) error {
		clear(out.Payload)
		return nil
	}, closure)
	if err != nil {
		t.Fatal(err)
	}
	go func() { defer close(returned); _, _ = consumer.Exchange(t.Context(), nil) }()
	<-started
	result, err := consumer.Cancel(t.Context())
	if err != nil || !result.TransportJoined {
		t.Fatalf("cleanup failed: %v", err)
	}
	<-returned
	if broker.exchanges.Load() != 1 || broker.cancels.Load() != 1 {
		t.Fatal("cleanup repeated input or target admission")
	}
}

// TestKubernetesStreamCancelJoinsInflightSink forbids a cleanup acknowledgement before the synchronous accepted-output callback returns.
// TestKubernetesStreamCancelJoinsInflightSink запрещает cleanup acknowledgement до возврата synchronous accepted-output callback.
func TestKubernetesStreamCancelJoinsInflightSink(t *testing.T) {
	started, returned := make(chan struct{}), make(chan struct{})
	var sinkReturned atomic.Bool
	broker := &gatewayStreamBrokerFixture{}
	broker.exchange = func(context.Context, *sb.ExchangeManagedKubernetesStreamRequest) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
		return gatewayStreamReply(), nil
	}
	broker.cancel = func(context.Context, *sb.CancelManagedKubernetesOperationRequest) (*sb.CancelManagedKubernetesOperationResponse, error) {
		if !sinkReturned.Load() {
			t.Error("cleanup bypassed actual sink join")
		}
		return &sb.CancelManagedKubernetesOperationResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_CANCELED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN, TransportJoined: true}, nil
	}
	binding, descriptor := gatewayStreamPins()
	sink := func(ctx context.Context, _ *sb.ManagedKubernetesInvocationBinding, _ *sb.ManagedKubernetesStreamDescriptor, out KubernetesStreamOutput) error {
		defer clear(out.Payload)
		close(started)
		<-ctx.Done()
		sinkReturned.Store(true)
		return ctx.Err()
	}
	consumer, err := NewKubernetesStreamConsumer(binding, descriptor, "kubernetes.exec", broker, sink, gatewayStreamClosure)
	if err != nil {
		t.Fatal(err)
	}
	go func() { defer close(returned); _, _ = consumer.Exchange(t.Context(), nil) }()
	<-started
	if _, err := consumer.Cancel(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-returned
	if consumer.acceptedOutput != 0 || broker.exchanges.Load() != 1 {
		t.Fatal("cancelled sink acknowledged output or replayed exchange")
	}
}
