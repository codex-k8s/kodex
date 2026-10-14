package providermodelcatalog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

type renewalWorkerRepository struct {
	port.ProviderCredentialRenewalRepository
	claims, completions            int
	tasks, recoveries              []port.ProviderCredentialRenewalTask
	result                         port.ProviderCredentialRenewalResult
	finalizeAlive, finalizeBounded bool
}

func (*renewalWorkerRepository) CheckProviderCredentialRenewalReadiness(context.Context) error {
	return nil
}
func (r *renewalWorkerRepository) ClaimProviderCredentialRenewalTasks(context.Context, string, port.ProviderCredentialRenewalEncoder) ([]port.ProviderCredentialRenewalTask, error) {
	r.claims++
	return r.tasks, nil
}
func (r *renewalWorkerRepository) ListProviderCredentialRenewalRecoveryTasks(ctx context.Context) ([]port.ProviderCredentialRenewalTask, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return r.recoveries, nil
}
func (r *renewalWorkerRepository) CompleteProviderCredentialRenewalTask(ctx context.Context, _ port.ProviderCredentialRenewalTask, result port.ProviderCredentialRenewalResult) error {
	r.completions++
	r.result = result
	r.finalizeAlive = ctx.Err() == nil
	deadline, ok := ctx.Deadline()
	r.finalizeBounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 5*time.Second
	return nil
}

type renewalWorkerClient struct {
	CredentialRenewer
	writes, reads int
	cancel        context.CancelFunc
}

func (c *renewalWorkerClient) RenewManagedProviderCredential(context.Context, port.ProviderCredentialRenewalTask) (port.ProviderCredentialRenewalResult, error) {
	c.writes++
	if c.cancel != nil {
		c.cancel()
		return port.ProviderCredentialRenewalResult{}, context.Canceled
	}
	return port.ProviderCredentialRenewalResult{State: "SUCCEEDED"}, nil
}
func (c *renewalWorkerClient) ObserveManagedProviderCredentialRenewal(context.Context, port.ProviderCredentialRenewalTask) (port.ProviderCredentialRenewalResult, error) {
	c.reads++
	return port.ProviderCredentialRenewalResult{State: "SUCCEEDED", Recovered: true}, nil
}

func TestRenewalWorkerStartupBarrierPreventsEffects(t *testing.T) {
	r, c := &renewalWorkerRepository{}, &renewalWorkerClient{}
	w, err := NewRenewalWorker(r, c, func(context.Context) error { return errors.New("startup unavailable") }, "fixture", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if w.runCycle(t.Context()) == nil || r.claims != 0 || c.writes != 0 || c.reads != 0 {
		t.Fatal("renewal crossed startup barrier")
	}
}

func TestRenewalWorkerCancellationPersistsUnknownWithIndependentBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := &renewalWorkerRepository{tasks: []port.ProviderCredentialRenewalTask{{Ref: "fixture"}}}
	c := &renewalWorkerClient{cancel: cancel}
	w, err := NewRenewalWorker(r, c, func(context.Context) error { return nil }, "fixture", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	_ = w.runCycle(ctx)
	if c.writes != 1 || c.reads != 0 || r.completions != 1 || r.result.State != "UNKNOWN_OUTCOME" || !r.finalizeAlive || !r.finalizeBounded {
		t.Fatal("cancelled renewal lost bounded durable completion")
	}
}

func TestRenewalWorkerRecoveryUsesReadWithoutRepeatingOAuth(t *testing.T) {
	r := &renewalWorkerRepository{recoveries: []port.ProviderCredentialRenewalTask{{Ref: "fixture"}}}
	c := &renewalWorkerClient{}
	w, err := NewRenewalWorker(r, c, func(context.Context) error { return nil }, "fixture", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if w.runCycle(t.Context()) != nil || c.writes != 0 || c.reads != 1 || r.completions != 1 || !r.result.Recovered {
		t.Fatal("renewal recovery repeated WRITE or lost READ evidence")
	}
}
