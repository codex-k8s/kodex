package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/callback"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type leaseRecoveryClient struct {
	runtimeWorkClientStub
	calls         atomic.Int32
	renewResponse func(context.Context, *controlplanev1.RenewExecutionRequest, int32) (*controlplanev1.RenewExecutionResponse, error)
}

func (client *leaseRecoveryClient) RenewExecution(ctx context.Context, request *controlplanev1.RenewExecutionRequest, _ ...grpc.CallOption) (*controlplanev1.RenewExecutionResponse, error) {
	return client.renewResponse(ctx, request, client.calls.Add(1))
}

func TestLeaseRenewalRecoversOnlyExactAcknowledgedLease(t *testing.T) {
	for _, phase := range []renewalPhase{renewalPhaseMaterialization, renewalPhaseTrack, renewalPhaseDrain} {
		for _, code := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded} {
			t.Run(string(phase)+"/"+code.String(), func(t *testing.T) {
				input := runtimeTrackingInput()
				prior := time.Now().Add(time.Second)
				ownerExpiry := time.Now().Add(time.Minute)
				client := &leaseRecoveryClient{renewResponse: func(ctx context.Context, request *controlplanev1.RenewExecutionRequest, n int32) (*controlplanev1.RenewExecutionResponse, error) {
					if request.LeaseRef != input.LeaseRef || request.Fence != input.LeaseFence || request.Generation != input.LeaseGeneration {
						t.Fatal("renewal changed exact authority pins")
					}
					if deadline, ok := ctx.Deadline(); !ok || deadline.After(prior) {
						t.Fatal("renewal exceeded previously acknowledged expiry")
					}
					if n < 3 {
						return nil, status.Error(code, "temporary delivery failure")
					}
					return renewalTestResponse(request, ownerExpiry), nil
				}}
				runtime := &runtime{control: client, config: Config{RequestTimeout: time.Millisecond}}
				until, err := runtime.renewLease(t.Context(), input, phase, prior)
				if err != nil || client.calls.Load() != 3 || !until.After(prior) || !until.Before(ownerExpiry) {
					t.Fatal("bounded recovery failed or extended the owner expiry")
				}
			})
		}
	}
}

func TestLeaseRenewalDoesNotRetryWithoutAcknowledgedExpiry(t *testing.T) {
	client := &leaseRecoveryClient{renewResponse: func(context.Context, *controlplanev1.RenewExecutionRequest, int32) (*controlplanev1.RenewExecutionResponse, error) {
		return nil, status.Error(codes.Unavailable, "no owner acknowledgement")
	}}
	runtime := &runtime{control: client, config: Config{RequestTimeout: time.Millisecond}}
	until, err := runtime.renewLease(t.Context(), runtimeTrackingInput(), renewalPhaseMaterialization, time.Time{})
	if err == nil || client.calls.Load() != 1 || !until.IsZero() {
		t.Fatal("unknown expiry became local authority or retry permission")
	}
}

func TestLeaseRenewalDenialsAreNeverRetried(t *testing.T) {
	for _, code := range []codes.Code{codes.PermissionDenied, codes.NotFound, codes.Unauthenticated, codes.FailedPrecondition, codes.InvalidArgument,
		codes.Canceled, codes.Internal, codes.DataLoss, codes.Aborted, codes.Unknown} {
		t.Run(code.String(), func(t *testing.T) {
			client := &leaseRecoveryClient{renewResponse: func(context.Context, *controlplanev1.RenewExecutionRequest, int32) (*controlplanev1.RenewExecutionResponse, error) {
				return nil, status.Error(code, "closed rejection")
			}}
			runtime := &runtime{control: client, config: Config{RequestTimeout: time.Millisecond}}
			prior := time.Now().Add(time.Second)
			until, err := runtime.renewLease(t.Context(), runtimeTrackingInput(), renewalPhaseTrack, prior)
			if status.Code(err) != code || client.calls.Load() != 1 || !until.Equal(prior) {
				t.Fatal("authority denial retried or changed expiry")
			}
		})
	}
}

func TestLeaseRenewalRejectsMalformedOrForeignSuccess(t *testing.T) {
	for _, name := range []string{"nil", "lease_nil", "ref", "fence", "generation", "expiry_nil", "expiry_invalid", "expiry_past"} {
		t.Run(name, func(t *testing.T) {
			client := &leaseRecoveryClient{renewResponse: func(_ context.Context, request *controlplanev1.RenewExecutionRequest, _ int32) (*controlplanev1.RenewExecutionResponse, error) {
				response := renewalTestResponse(request, time.Now().Add(time.Minute))
				switch name {
				case "nil":
					response = nil
				case "lease_nil":
					response.Lease = nil
				case "ref":
					response.Lease.Ref += "_foreign"
				case "fence":
					response.Lease.Fence += "_foreign"
				case "generation":
					response.Lease.Generation++
				case "expiry_nil":
					response.Lease.ExpiresAt = nil
				case "expiry_invalid":
					response.Lease.ExpiresAt = &timestamppb.Timestamp{Nanos: -1}
				case "expiry_past":
					response.Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
				}
				return response, nil
			}}
			runtime := &runtime{control: client, config: Config{RequestTimeout: time.Millisecond}}
			prior := time.Now().Add(time.Second)
			until, err := runtime.renewLease(t.Context(), runtimeTrackingInput(), renewalPhaseTrack, prior)
			if !errors.Is(err, errLeaseRenewalBinding) || client.calls.Load() != 1 || !until.Equal(prior) {
				t.Fatal("invalid owner response became acknowledged authority")
			}
		})
	}
}

func TestLeaseRenewalOutageIsBoundedAndDoesNotExtendExpiry(t *testing.T) {
	client := &leaseRecoveryClient{renewResponse: func(context.Context, *controlplanev1.RenewExecutionRequest, int32) (*controlplanev1.RenewExecutionResponse, error) {
		return nil, status.Error(codes.Unavailable, "outage")
	}}
	runtime := &runtime{control: client, config: Config{RequestTimeout: time.Millisecond}}
	prior := time.Now().Add(time.Second)
	until, err := runtime.renewLease(t.Context(), runtimeTrackingInput(), renewalPhaseTrack, prior)
	if err == nil || client.calls.Load() != maximumLeaseRenewalTries || !until.Equal(prior) {
		t.Fatal("unbounded retry or locally extended expiry")
	}
}

func TestLeaseRenewalExpiryCancelsInFlightAndRejectsLateSuccess(t *testing.T) {
	client := &leaseRecoveryClient{renewResponse: func(ctx context.Context, request *controlplanev1.RenewExecutionRequest, _ int32) (*controlplanev1.RenewExecutionResponse, error) {
		<-ctx.Done()
		// Некорректный adapter сообщает success после context deadline.
		return renewalTestResponse(request, time.Now().Add(time.Hour)), nil
	}}
	runtime := &runtime{control: client, config: Config{RequestTimeout: time.Second}}
	prior := time.Now().Add(10 * time.Millisecond)
	until, err := runtime.renewLease(t.Context(), runtimeTrackingInput(), renewalPhaseTrack, prior)
	if !errors.Is(err, context.DeadlineExceeded) || client.calls.Load() != 1 || !until.Equal(prior) {
		t.Fatal("late response resurrected expired authority")
	}
}

func TestLeaseRenewalCancelledOrExpiredParentDoesNotCallOwner(t *testing.T) {
	for _, expired := range []bool{false, true} {
		client := &leaseRecoveryClient{renewResponse: func(context.Context, *controlplanev1.RenewExecutionRequest, int32) (*controlplanev1.RenewExecutionResponse, error) {
			t.Fatal("closed parent called owner")
			return nil, nil
		}}
		runtime := &runtime{control: client, config: Config{RequestTimeout: time.Millisecond}}
		ctx, cancel := context.WithCancel(t.Context())
		prior := time.Now().Add(time.Second)
		if expired {
			prior = time.Now().Add(-time.Second)
		} else {
			cancel()
		}
		_, err := runtime.renewLease(ctx, runtimeTrackingInput(), renewalPhaseTrack, prior)
		cancel()
		if err == nil || client.calls.Load() != 0 {
			t.Fatal("closed parent renewed lease")
		}
	}
}

func TestTrackRecoveryDoesNotDeleteSuccessfulCallback(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &leaseRecoveryClient{renewResponse: func(_ context.Context, request *controlplanev1.RenewExecutionRequest, n int32) (*controlplanev1.RenewExecutionResponse, error) {
		if n == 1 {
			return nil, status.Error(codes.Unavailable, "transient")
		}
		coordinator.Complete(input.LeaseRef)
		return renewalTestResponse(request, time.Now().Add(time.Minute)), nil
	}}
	turns := &turnLifecycleStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.config.RequestTimeout, runtime.config.LeaseRenewInterval, runtime.inspectInterval = 100*time.Millisecond, time.Millisecond, time.Hour
	runtime.track(t.Context(), input, done, false, time.Now().Add(time.Second))
	if client.calls.Load() != 2 || turns.stops.Load() != 0 || turns.deletions.Load() != 0 || client.count() != 0 || len(runtime.capacity) != 0 {
		t.Fatal("transient RPC deleted callback material or produced synthetic completion")
	}
}

func TestTrackExpiryStopsWithoutWaitingForRenewTicker(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &runtimeWorkClientStub{}
	turns := &turnLifecycleStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.inspectInterval = time.Hour
	runtime.track(t.Context(), input, done, false, time.Now().Add(10*time.Millisecond))
	if turns.stops.Load() != 1 || turns.deletions.Load() != 1 || client.count() != 0 {
		t.Fatal("expiry waited for periodic renew or fabricated owner completion")
	}
}

func TestMaterializationRecoveryAndExpiryPreservePublicationFence(t *testing.T) {
	var calls atomic.Int32
	client := &materializationRenewClient{renew: func(context.Context, *controlplanev1.RenewExecutionRequest) error {
		switch calls.Add(1) {
		case 2:
			return status.Error(codes.Unavailable, "transient")
		case 4:
			return status.Error(codes.PermissionDenied, "revoked")
		default:
			return nil
		}
	}}
	runtime := keeperTestRuntime(client)
	runtime.config.RequestTimeout, runtime.config.LeaseRenewInterval = 100*time.Millisecond, time.Hour
	keeper := runtime.keepMaterializationLease(t.Context(), runtimeTrackingInput())
	defer keeper.stop()
	if keeper.await() != nil {
		t.Fatal("initial renewal failed")
	}
	called := false
	if keeper.publish(func(context.Context) error { called = true; return nil }) != nil || !called || calls.Load() != 3 {
		t.Fatal("publication failed to recover transient delivery within acknowledged expiry")
	}
	called = false
	if status.Code(keeper.publish(func(context.Context) error { called = true; return nil })) != codes.PermissionDenied || called || calls.Load() != 4 {
		t.Fatal("publication passed revoked exact lease")
	}
}

func TestMaterializationExpiryWatchdogClosesBeforePublication(t *testing.T) {
	client := &leaseRecoveryClient{renewResponse: func(_ context.Context, request *controlplanev1.RenewExecutionRequest, _ int32) (*controlplanev1.RenewExecutionResponse, error) {
		return renewalTestResponse(request, time.Now().Add(100*time.Millisecond)), nil
	}}
	runtime := &runtime{control: client, config: Config{RequestTimeout: time.Second, LeaseRenewInterval: time.Hour}}
	keeper := runtime.keepMaterializationLease(t.Context(), runtimeTrackingInput())
	defer keeper.stop()
	if keeper.await() != nil {
		t.Fatal("initial renewal failed")
	}
	awaitKeeperTest(t, keeper.done)
	called := false
	if keeper.publish(func(context.Context) error { called = true; return nil }) == nil || called || client.calls.Load() != 1 {
		t.Fatal("expired lease published workload")
	}
}

func TestDrainRecoveryPreservesSuccessfulCallbackAndJoins(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &leaseRecoveryClient{renewResponse: func(_ context.Context, request *controlplanev1.RenewExecutionRequest, n int32) (*controlplanev1.RenewExecutionResponse, error) {
		if n == 1 {
			return nil, status.Error(codes.Unavailable, "transient")
		}
		coordinator.Complete(input.LeaseRef)
		return renewalTestResponse(request, time.Now().Add(time.Minute)), nil
	}}
	turns := &turnLifecycleStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.config.RequestTimeout, runtime.config.LeaseRenewInterval, runtime.terminalGrace = 100*time.Millisecond, time.Millisecond, time.Second
	runtime.drainTurn(t.Context(), input, done, time.Now().Add(time.Second))
	if client.calls.Load() != 2 || turns.stops.Load() != 1 || turns.deletions.Load() != 0 || client.count() != 0 {
		t.Fatal("drain recovery discarded callback or fabricated timeout completion")
	}
}

func TestDrainGraceDeadlineStopsRecoveryAndPreservesTimeoutReceipt(t *testing.T) {
	input := runtimeTrackingInput()
	coordinator := callback.NewCoordinator()
	done := coordinator.Register(input)
	client := &leaseRecoveryClient{renewResponse: func(ctx context.Context, _ *controlplanev1.RenewExecutionRequest, _ int32) (*controlplanev1.RenewExecutionResponse, error) {
		<-ctx.Done()
		return nil, status.Error(codes.DeadlineExceeded, "grace expired")
	}}
	turns := &turnLifecycleStub{}
	runtime := trackingRuntime(client, turns, coordinator)
	runtime.config.RequestTimeout, runtime.config.LeaseRenewInterval, runtime.terminalGrace = time.Second, time.Millisecond, 20*time.Millisecond
	runtime.drainTurn(t.Context(), input, done, time.Now().Add(time.Second))
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.calls.Load() != 1 || turns.stops.Load() != 1 || turns.deletions.Load() != 1 || len(client.requests) != 1 || client.requests[0].ResultSummary != "i18n:RUNTIME_TIMEOUT" {
		t.Fatalf("grace budget result: renew=%d stops=%d delete=%d receipts=%d", client.calls.Load(), turns.stops.Load(), turns.deletions.Load(), len(client.requests))
	}
}
