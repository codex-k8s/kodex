package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	client "github.com/codex-k8s/matter-codex/services/jobs/automation-scheduler/internal/clients/controlplane"
	internalobservability "github.com/codex-k8s/matter-codex/services/jobs/automation-scheduler/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
)

type schedulerTestControlPlane struct {
	claims            []client.Claim
	claimIndex        int
	materializeErrors []error
	materializeIndex  int
	claimErrors       []error
	claimErrorIndex   int
	materializeKeys   []string
	claimKeys         []string
	completionError   map[string]error
	completed         []string
}

func (fake *schedulerTestControlPlane) MaterializeDue(_ context.Context, key string, _ int) (int, error) {
	fake.materializeKeys = append(fake.materializeKeys, key)
	if fake.materializeIndex < len(fake.materializeErrors) {
		err := fake.materializeErrors[fake.materializeIndex]
		fake.materializeIndex++
		return 0, err
	}
	return 2, nil
}

func (fake *schedulerTestControlPlane) ClaimNext(_ context.Context, key string) (client.Claim, error) {
	fake.claimKeys = append(fake.claimKeys, key)
	if fake.claimErrorIndex < len(fake.claimErrors) {
		err := fake.claimErrors[fake.claimErrorIndex]
		fake.claimErrorIndex++
		return client.Claim{}, err
	}
	if fake.claimIndex >= len(fake.claims) {
		return client.Claim{}, client.ErrNoWork
	}
	claim := fake.claims[fake.claimIndex]
	fake.claimIndex++
	return claim, nil
}

func (fake *schedulerTestControlPlane) Complete(
	_ context.Context,
	claim client.Claim,
	_ string,
) (string, error) {
	fake.completed = append(fake.completed, claim.OccurrenceID)
	if err := fake.completionError[claim.OccurrenceID]; err != nil {
		return "", err
	}
	return "SCHEDULE_OCCURRENCE_STATE_SUCCEEDED", nil
}

func TestCycleIsolatesTrackedClaimFailureAndContinuesBacklog(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := internalobservability.New(func(collectors ...prometheus.Collector) error {
		for _, collector := range collectors {
			if registerErr := registry.Register(collector); registerErr != nil {
				return registerErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}
	now := time.Now().UTC()
	fake := &schedulerTestControlPlane{
		claims: []client.Claim{{
			OccurrenceID: "new-occurrence", Attempt: 1,
			LeaseToken: "new-token", LeaseExpiresAt: now.Add(time.Minute),
		}},
		completionError: map[string]error{"broken-occurrence": errors.New("row failure")},
	}
	job, err := New(fake, metrics, Config{
		DueLimit: 10, ClaimLimit: 10, MaximumTrackedClaims: 20,
	})
	if err != nil {
		t.Fatalf("create scheduler: %v", err)
	}
	job.claims["broken-occurrence"] = client.Claim{
		OccurrenceID: "broken-occurrence", Attempt: 1,
		LeaseToken: "broken-token", LeaseExpiresAt: now.Add(time.Minute),
	}
	job.claims["terminal-occurrence"] = client.Claim{
		OccurrenceID: "terminal-occurrence", Attempt: 1,
		LeaseToken: "terminal-token", LeaseExpiresAt: now.Add(time.Minute),
	}

	cycleErr := job.Cycle(context.Background())
	if cycleErr == nil {
		t.Fatal("cycle did not report the isolated row failure")
	}
	if _, exists := job.claims["terminal-occurrence"]; exists {
		t.Fatal("terminal occurrence remained transiently tracked")
	}
	if _, exists := job.claims["broken-occurrence"]; !exists {
		t.Fatal("failed occurrence lost its server-issued claim before deadline")
	}
	if _, exists := job.claims["new-occurrence"]; !exists {
		t.Fatal("row failure blocked claiming the remaining backlog")
	}
	if len(fake.completed) != 2 {
		t.Fatalf("cycle did not reconcile all tracked claims: %v", fake.completed)
	}
}

func TestCompletionKeyIsStablePerOccurrenceAttempt(t *testing.T) {
	claim := client.Claim{OccurrenceID: "occurrence", Attempt: 3}
	first := completionKey(claim)
	second := completionKey(claim)
	if first == "" || first != second {
		t.Fatalf("completion key is not stable: %q %q", first, second)
	}
	claim.Attempt++
	if completionKey(claim) == first {
		t.Fatal("new attempt reused the previous semantic idempotency key")
	}
}

func TestExpiredClaimIsReleasedWithoutLocalTerminalDecision(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := internalobservability.New(func(collectors ...prometheus.Collector) error {
		for _, collector := range collectors {
			if registerErr := registry.Register(collector); registerErr != nil {
				return registerErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}
	fake := &schedulerTestControlPlane{
		completionError: map[string]error{"expired-occurrence": errors.New("row failure")},
	}
	job, err := New(fake, metrics, Config{
		DueLimit: 10, ClaimLimit: 10, MaximumTrackedClaims: 20,
	})
	if err != nil {
		t.Fatalf("create scheduler: %v", err)
	}
	job.claims["expired-occurrence"] = client.Claim{
		OccurrenceID: "expired-occurrence", Attempt: 1,
		LeaseToken: "expired-token", LeaseExpiresAt: time.Now().UTC().Add(-time.Second),
	}

	if cycleErr := job.Cycle(context.Background()); cycleErr != nil {
		t.Fatalf("expired transient claim became a local terminal error: %v", cycleErr)
	}
	if _, exists := job.claims["expired-occurrence"]; exists {
		t.Fatal("expired claim remained transiently tracked")
	}
}

func TestUnknownOutcomeKeepsSemanticKeysAcrossCycles(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := internalobservability.New(func(collectors ...prometheus.Collector) error {
		for _, collector := range collectors {
			if registerErr := registry.Register(collector); registerErr != nil {
				return registerErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("create metrics: %v", err)
	}
	fake := &schedulerTestControlPlane{
		materializeErrors: []error{errors.New("unknown due outcome")},
		claimErrors:       []error{errors.New("unknown claim outcome")},
		claims: []client.Claim{{
			OccurrenceID: "replayed-occurrence", Attempt: 1,
			LeaseToken: "replayed-token", LeaseExpiresAt: time.Now().UTC().Add(time.Minute),
		}},
	}
	job, err := New(fake, metrics, Config{
		DueLimit: 10, ClaimLimit: 10, MaximumTrackedClaims: 20,
	})
	if err != nil {
		t.Fatalf("create scheduler: %v", err)
	}

	if cycleErr := job.Cycle(context.Background()); cycleErr == nil {
		t.Fatal("unknown outcomes were not reported")
	}
	if cycleErr := job.Cycle(context.Background()); cycleErr != nil {
		t.Fatalf("semantic replay did not recover: %v", cycleErr)
	}
	if len(fake.materializeKeys) != 2 || fake.materializeKeys[0] != fake.materializeKeys[1] {
		t.Fatalf("due materialization changed semantic key: %v", fake.materializeKeys)
	}
	if len(fake.claimKeys) < 2 || fake.claimKeys[0] != fake.claimKeys[1] {
		t.Fatalf("occurrence claim changed semantic key: %v", fake.claimKeys)
	}
	if _, exists := job.claims["replayed-occurrence"]; !exists {
		t.Fatal("replayed occurrence was not transiently tracked")
	}
}
