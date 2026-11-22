package capture

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// captureRepositoryFixture records actual claims after slot reservation, without minting authority.
// captureRepositoryFixture учитывает actual claims после slot reservation, не создавая authority.
type captureRepositoryFixture struct {
	claims     atomic.Int32
	claim      *Claim
	heartbeats atomic.Int32
}

// ClaimCapture returns the test's independently issued task and records each claim RPC equivalent.
// ClaimCapture возвращает independently issued test task и учитывает каждый claim.
func (repository *captureRepositoryFixture) ClaimCapture(context.Context, string, int64) (*Claim, error) {
	repository.claims.Add(1)
	return repository.claim, nil
}

// HeartbeatCapture records compatibility only after a successful producer check.
// HeartbeatCapture учитывает compatibility только после успешной producer check.
func (repository *captureRepositoryFixture) HeartbeatCapture(context.Context, string, uint32) error {
	repository.heartbeats.Add(1)
	return nil
}

// captureOwnerFixture supplies bounded owner outcomes and observes recovery/renewal calls.
// captureOwnerFixture задаёт bounded owner outcomes и наблюдает recovery/renewal calls.
type captureOwnerFixture struct {
	finalize      func(context.Context) (managedkubernetes.CaptureTaskReceipt, error)
	read          func(context.Context) (managedkubernetes.CaptureTaskReceipt, error)
	renew         func(context.Context) (time.Time, error)
	revision      uint32
	reads, renews atomic.Int32
}

// Check returns the explicit producer compatibility revision.
// Check возвращает явную producer compatibility revision.
func (owner *captureOwnerFixture) Check(context.Context) (uint32, error) { return owner.revision, nil }

// Finalize executes exactly the supplied one-shot owner outcome.
// Finalize исполняет ровно заданный one-shot owner outcome.
func (owner *captureOwnerFixture) Finalize(ctx context.Context, _ managedkubernetes.CaptureTaskBinding) (managedkubernetes.CaptureTaskReceipt, error) {
	return owner.finalize(ctx)
}

// Read observes metadata-only recovery without resubmitting Finalize.
// Read наблюдает metadata-only recovery без повторного Finalize.
func (owner *captureOwnerFixture) Read(ctx context.Context, _ managedkubernetes.CaptureTaskBinding) (managedkubernetes.CaptureTaskReceipt, error) {
	owner.reads.Add(1)
	return owner.read(ctx)
}

// Renew observes the bounded worker and can force loss of the exact attempt.
// Renew наблюдает bounded worker и может подтвердить утрату exact attempt.
func (owner *captureOwnerFixture) Renew(ctx context.Context, _ managedkubernetes.CaptureTaskBinding) (time.Time, error) {
	owner.renews.Add(1)
	return owner.renew(ctx)
}

// captureTaskFixture creates a finite original task and a matching complete empty receipt.
// captureTaskFixture создаёт конечную original task и соответствующий complete empty receipt.
func captureTaskFixture() (*Claim, managedkubernetes.CaptureTaskReceipt) {
	binding := managedkubernetes.CaptureTaskBinding{CaptureRef: "koc_abcdefgh12345678", LeaseRef: "kca_abcdefgh12345678", Fence: strings.Repeat("f", 64), Generation: 1, WorkloadInstance: "retention-0", SnapshotDigest: strings.Repeat("a", 64)}
	digest, _ := binding.Digest()
	return &Claim{Binding: binding, ExpiresAt: time.Now().Add(2 * time.Minute)}, managedkubernetes.CaptureTaskReceipt{CaptureRef: binding.CaptureRef, StreamRef: "kst_abcdefgh12345678", SnapshotDigest: binding.SnapshotDigest, TaskBindingDigest: digest, TaskGeneration: 1, CaptureGeneration: 1, State: managedkubernetes.CaptureComplete, Kind: managedkubernetes.CaptureEmpty}
}

// TestCaptureLostFinalizeACKUsesOneIndependentRead preserves one-shot finalization after cancel.
// TestCaptureLostFinalizeACKUsesOneIndependentRead сохраняет one-shot finalization после cancel.
func TestCaptureLostFinalizeACKUsesOneIndependentRead(t *testing.T) {
	claim, receipt := captureTaskFixture()
	repository := &captureRepositoryFixture{claim: claim}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner := &captureOwnerFixture{finalize: func(context.Context) (managedkubernetes.CaptureTaskReceipt, error) {
		cancel()
		return managedkubernetes.CaptureTaskReceipt{}, ErrUncertain
	}, read: func(ctx context.Context) (managedkubernetes.CaptureTaskReceipt, error) {
		if ctx.Err() != nil {
			t.Error("recovery inherited cancellation")
		}
		return receipt, nil
	}}
	result, err := NewProcessor(repository, owner).Process(ctx, "retention-0", 120)
	if err != nil || result.Complete != 1 || repository.claims.Load() != 1 || owner.reads.Load() != 1 {
		t.Fatal("lost acknowledgement was replayed or discarded", result, err)
	}
}

// TestCapturePositiveMalformedReplyNeverHidesBehindRead refuses a changed task binding.
// TestCapturePositiveMalformedReplyNeverHidesBehindRead отклоняет изменённый task binding.
func TestCapturePositiveMalformedReplyNeverHidesBehindRead(t *testing.T) {
	claim, receipt := captureTaskFixture()
	receipt.TaskGeneration++
	owner := &captureOwnerFixture{finalize: func(context.Context) (managedkubernetes.CaptureTaskReceipt, error) { return receipt, nil }}
	_, err := NewProcessor(&captureRepositoryFixture{claim: claim}, owner).Process(t.Context(), "retention-0", 120)
	if !errors.Is(err, ErrMalformed) || owner.reads.Load() != 0 {
		t.Fatal("malformed positive reply was recovered", err)
	}
}

// TestCaptureReservesSlotBeforeClaim prevents a busy worker from abandoning leased tasks.
// TestCaptureReservesSlotBeforeClaim не позволяет занятому worker бросать leased tasks.
func TestCaptureReservesSlotBeforeClaim(t *testing.T) {
	claim, receipt := captureTaskFixture()
	repository := &captureRepositoryFixture{claim: claim}
	entered := make(chan struct{})
	release := make(chan struct{})
	owner := &captureOwnerFixture{finalize: func(context.Context) (managedkubernetes.CaptureTaskReceipt, error) {
		close(entered)
		<-release
		return receipt, nil
	}}
	processor := NewProcessor(repository, owner)
	done := make(chan error, 1)
	go func() { _, err := processor.Process(t.Context(), "retention-0", 120); done <- err }()
	<-entered
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := processor.Process(cancelled, "retention-0", 120)
	if !errors.Is(err, context.Canceled) || repository.claims.Load() != 1 {
		t.Error("busy processor claimed another task", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestCaptureRenewLossCancelsAndJoinsBeforeReturn proves no background task survives its owner.
// TestCaptureRenewLossCancelsAndJoinsBeforeReturn проверяет завершение background task до owner.
func TestCaptureRenewLossCancelsAndJoinsBeforeReturn(t *testing.T) {
	claim, _ := captureTaskFixture()
	joined := make(chan struct{})
	owner := &captureOwnerFixture{finalize: func(ctx context.Context) (managedkubernetes.CaptureTaskReceipt, error) {
		<-ctx.Done()
		return managedkubernetes.CaptureTaskReceipt{}, ErrLostClaim
	}, renew: func(context.Context) (time.Time, error) { defer close(joined); return time.Time{}, ErrLostClaim }}
	processor := NewProcessor(&captureRepositoryFixture{claim: claim}, owner)
	processor.renewEvery = time.Millisecond
	_, err := processor.Process(t.Context(), "retention-0", 120)
	if !errors.Is(err, ErrLostClaim) {
		t.Fatal(err)
	}
	select {
	case <-joined:
	default:
		t.Fatal("renewal returned before positive join")
	}
	if owner.renews.Load() != 1 || owner.reads.Load() != 0 {
		t.Fatal("lost lease caused a retry")
	}
}

// TestCaptureUnavailableProducerNeverPublishesConsumerHeartbeat distinguishes not-ready from bad wire.
// TestCaptureUnavailableProducerNeverPublishesConsumerHeartbeat отличает not-ready от bad wire.
func TestCaptureUnavailableProducerNeverPublishesConsumerHeartbeat(t *testing.T) {
	repository := &captureRepositoryFixture{}
	processor := NewProcessor(repository, &captureOwnerFixture{})
	if !errors.Is(processor.Check(t.Context(), "retention-0"), ErrNotReady) || repository.heartbeats.Load() != 0 {
		t.Fatal("unavailable producer published readiness")
	}
}
