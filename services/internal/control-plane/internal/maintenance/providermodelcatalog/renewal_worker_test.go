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

// renewalWorkerRepository captures synthetic renewal claims/completions, recovery lists and
// whether finalization retains an active independent five-second budget.
// renewalWorkerRepository сохраняет синтетические renewal claims и completions, recovery lists и
// наличие активного независимого пятисекундного бюджета финализации.
type renewalWorkerRepository struct {
	port.ProviderCredentialRenewalRepository
	claims, completions            int
	tasks, recoveries              []port.ProviderCredentialRenewalTask
	result                         port.ProviderCredentialRenewalResult
	finalizeAlive, finalizeBounded bool
}

// CheckProviderCredentialRenewalReadiness returns synthetic ready state without contacting the
// provider or a live authoritative repository.
// CheckProviderCredentialRenewalReadiness возвращает синтетическую готовность без обращения к
// provider либо живому авторитетному repository.
func (*renewalWorkerRepository) CheckProviderCredentialRenewalReadiness(context.Context) error {
	return nil
}

// ClaimProviderCredentialRenewalTasks counts one synthetic renewal claim and returns the
// configured task slice for controlled write-path tests.
// ClaimProviderCredentialRenewalTasks считает один синтетический renewal claim и возвращает
// заданный срез task для контролируемых write-path тестов.
func (r *renewalWorkerRepository) ClaimProviderCredentialRenewalTasks(context.Context, string, port.ProviderCredentialRenewalEncoder) ([]port.ProviderCredentialRenewalTask, error) {
	r.claims++
	return r.tasks, nil
}

// ListProviderCredentialRenewalRecoveryTasks returns configured synthetic recovery tasks only
// while the supplied parent context remains active.
// ListProviderCredentialRenewalRecoveryTasks возвращает заданные синтетические recovery tasks
// только пока переданный parent context остаётся активным.
func (r *renewalWorkerRepository) ListProviderCredentialRenewalRecoveryTasks(ctx context.Context) ([]port.ProviderCredentialRenewalTask, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return r.recoveries, nil
}

// CompleteProviderCredentialRenewalTask captures the typed result and verifies by observation that
// the supplied finalization context is alive with a positive budget of at most five seconds.
// CompleteProviderCredentialRenewalTask сохраняет типизированный результат и наблюдает активность
// переданного finalization context с положительным бюджетом не более пяти секунд.
func (r *renewalWorkerRepository) CompleteProviderCredentialRenewalTask(ctx context.Context, _ port.ProviderCredentialRenewalTask, result port.ProviderCredentialRenewalResult) error {
	r.completions++
	r.result = result
	r.finalizeAlive = ctx.Err() == nil
	deadline, ok := ctx.Deadline()
	r.finalizeBounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 5*time.Second
	return nil
}

// renewalWorkerClient counts renewal writes and recovery reads and can cancel the caller during a
// simulated write without real OAuth effects.
// renewalWorkerClient считает renewal writes и recovery reads и может отменить caller во время
// имитации записи без реальных OAuth эффектов.
type renewalWorkerClient struct {
	CredentialRenewer
	writes, reads int
	cancel        context.CancelFunc
}

// RenewManagedProviderCredential counts a synthetic write, optionally cancels the parent with an
// uncertain cancellation result, or returns synthetic SUCCEEDED.
// RenewManagedProviderCredential считает синтетическую запись, при необходимости отменяет parent с
// неопределённым результатом отмены либо возвращает синтетический SUCCEEDED.
func (c *renewalWorkerClient) RenewManagedProviderCredential(context.Context, port.ProviderCredentialRenewalTask) (port.ProviderCredentialRenewalResult, error) {
	c.writes++
	if c.cancel != nil {
		c.cancel()
		return port.ProviderCredentialRenewalResult{}, context.Canceled
	}
	return port.ProviderCredentialRenewalResult{State: "SUCCEEDED"}, nil
}

// ObserveManagedProviderCredentialRenewal counts a synthetic recovery read and returns SUCCEEDED
// with recovered provenance without repeating the write.
// ObserveManagedProviderCredentialRenewal считает синтетическое recovery чтение и возвращает
// SUCCEEDED с recovered происхождением без повторения записи.
func (c *renewalWorkerClient) ObserveManagedProviderCredentialRenewal(context.Context, port.ProviderCredentialRenewalTask) (port.ProviderCredentialRenewalResult, error) {
	c.reads++
	return port.ProviderCredentialRenewalResult{State: "SUCCEEDED", Recovered: true}, nil
}

// TestRenewalWorkerStartupBarrierPreventsEffects checks a failed startup barrier prevents owner
// claims, renewal writes and recovery reads.
// TestRenewalWorkerStartupBarrierPreventsEffects проверяет запрет owner claims, renewal writes и
// recovery reads при отказе startup barrier.
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

// TestRenewalWorkerCancellationPersistsUnknownWithIndependentBudget checks cancellation during a
// write still records exactly one UNKNOWN_OUTCOME completion under a fresh active bounded context
// with no recovery read.
// TestRenewalWorkerCancellationPersistsUnknownWithIndependentBudget проверяет сохранение ровно
// одного UNKNOWN_OUTCOME completion при отмене записи под свежим активным ограниченным context без
// recovery чтения.
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

// TestRenewalWorkerRecoveryUsesReadWithoutRepeatingOAuth checks a recovery-only task performs one
// read and one recovered completion with zero OAuth write attempts.
// TestRenewalWorkerRecoveryUsesReadWithoutRepeatingOAuth проверяет одно чтение и один recovered
// completion recovery-only task при отсутствии попыток OAuth записи.
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
