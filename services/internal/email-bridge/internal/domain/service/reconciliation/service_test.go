package reconciliation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
)

// storeFixture overrides pending/claim/commit for isolated worker tests; other embedded repository methods are not implemented.
// storeFixture подставляет pending/claim/commit для изолированных тестов worker; остальные встроенные методы не реализованы.
type storeFixture struct {
	receipt.ReconciliationRepository
	pending func(context.Context, int) ([]receipt.Pending, error)
	commit  func(context.Context, receipt.Pending, receipt.Decision) (bool, error)
}

// Pending forwards the cycle context and batch bound to the case-specific callback.
// Pending передаёт контекст цикла и размер пачки callback конкретного сценария.
func (f storeFixture) Pending(ctx context.Context, b int) ([]receipt.Pending, error) {
	return f.pending(ctx, b)
}

// ClaimReconciliation always grants the synthetic claim; tests using it do not prove concurrent storage fencing.
// ClaimReconciliation всегда выдаёт синтетический claim; использующие его тесты не доказывают конкурентное ограничение в хранилище.
func (f storeFixture) ClaimReconciliation(context.Context, receipt.Pending, time.Duration) (bool, error) {
	return true, nil
}

// CommitReconciliation delegates exact pending/decision values and cancellation to the assertion callback.
// CommitReconciliation передаёт точные pending/decision и отмену проверяющему callback.
func (f storeFixture) CommitReconciliation(ctx context.Context, p receipt.Pending, d receipt.Decision) (bool, error) {
	return f.commit(ctx, p, d)
}

// ownerFixture substitutes decision resolution only, without a real owner RPC or report implementation.
// ownerFixture подставляет только разрешение решения, без реального owner RPC или реализации отчёта.
type ownerFixture struct {
	receipt.EffectAuthority
	resolve func(context.Context, receipt.OwnerReceipt, string) (receipt.Decision, error)
}

// Reconcile exposes the exact receipt, selected decision reference and context to the synthetic owner callback.
// Reconcile передаёт точную квитанцию, ссылку выбранного решения и контекст синтетическому callback владельца.
func (f ownerFixture) Reconcile(ctx context.Context, p receipt.OwnerReceipt, d string) (receipt.Decision, error) {
	return f.resolve(ctx, p, d)
}

// observer counts worker outcomes synchronously; callers inspect it after the worker has joined.
// observer синхронно считает исходы worker; вызывающий читает его после завершения worker.
type observer map[string]int

// Reconciliation increments the selected test outcome count without performing I/O or collecting payloads.
// Reconciliation увеличивает счётчик выбранного тестового исхода без I/O или сбора содержимого.
func (o observer) Reconciliation(v string) { o[v]++ }

// fixture constructs a correlated unknown receipt and an unexpired synthetic conclusive decision for worker assertions.
// fixture создаёт связную неопределённую квитанцию и неистёкшее синтетическое определённое решение для проверок worker.
func fixture() (receipt.Pending, receipt.Decision) {
	r := receipt.Record{ID: "receipt", Key: "key", Digest: api.Digest("input"), Status: "unknown", Audit: receipt.Audit{Actor: "actor", Agent: "agent", Grant: "grant", Operation: api.OperationMarkRead, ConfigurationRevision: 1, CredentialGeneration: 1}}
	scope := receipt.Scope{Tenant: "tenant", Mailbox: "mailbox"}
	o := receipt.OwnerReceipt{Ref: "owner_receipt", Version: 1, Invocation: "invocation", ExternalRef: r.ID, ExternalDigest: r.ExternalDigest(scope), InputDigest: r.Digest, EffectKey: r.Key, Mailbox: scope.Mailbox, Connection: "connection", ConfigurationRevision: 1, Outcome: receipt.Unknown}
	p := receipt.Pending{Scope: scope, Record: r, Owner: o}
	d := receipt.Decision{Ref: "decision", Version: 1, Actor: "owner", Grant: "fresh", Receipt: o, Outcome: receipt.EffectConfirmed, ExpiresAt: time.Now().Add(time.Minute)}
	return p, d
}

// TestWorkerStartupBarrierAndCancelJoin cancels at barrier, read, owner RPC and commit checkpoints and waits for cooperative worker completion.
// It verifies no early commit and no dependency outliving shutdown when each injected dependency observes context cancellation.
// TestWorkerStartupBarrierAndCancelJoin отменяет barrier, чтение, owner RPC и commit и ожидает совместного завершения worker.
// Проверяются отсутствие раннего commit и завершение до закрытия зависимостей, когда каждая заглушка соблюдает отмену контекста.
func TestWorkerStartupBarrierAndCancelJoin(t *testing.T) {
	for _, phase := range []string{"barrier", "pending", "rpc", "commit"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered := make(chan struct{})
			done := make(chan error, 1)
			p, d := fixture()
			var commits atomic.Int32
			block := func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
			s := Service{Interval: 5 * time.Second, Batch: 1, Barrier: func(ctx context.Context) error {
				if phase == "barrier" {
					return block(ctx)
				}
				return nil
			}, Repository: storeFixture{pending: func(ctx context.Context, b int) ([]receipt.Pending, error) {
				if phase == "barrier" {
					t.Error("poll before startup barrier")
				}
				if phase == "pending" {
					return nil, block(ctx)
				}
				return []receipt.Pending{p}, nil
			}, commit: func(ctx context.Context, _ receipt.Pending, _ receipt.Decision) (bool, error) {
				commits.Add(1)
				return false, block(ctx)
			}}, Authority: ownerFixture{resolve: func(ctx context.Context, _ receipt.OwnerReceipt, _ string) (receipt.Decision, error) {
				if phase == "rpc" {
					return receipt.Decision{}, block(ctx)
				}
				return d, nil
			}}}
			go func() { done <- s.Run(ctx) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("worker did not enter expected phase")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("worker did not join before dependency close")
			}
			if phase != "commit" && commits.Load() != 0 {
				t.Fatal("cancelled path reached commit")
			}
		})
	}
}

// TestWorkerPartialProgressAndFreshAuthorization requires an explicit fresh decision recheck and retains the first commit when the next item fails.
// TestWorkerPartialProgressAndFreshAuthorization требует явную свежую проверку решения и сохраняет первый commit при отказе следующего элемента.
func TestWorkerPartialProgressAndFreshAuthorization(t *testing.T) {
	p, d := fixture()
	second := p
	second.Record.ID = "second"
	second.Owner.ExternalRef = "second"
	second.Owner.ExternalDigest = second.Record.ExternalDigest(second.Scope)
	o := observer{}
	calls := 0
	commits := 0
	s := Service{Batch: 2, Interval: 5 * time.Second, Observer: o, Repository: storeFixture{pending: func(context.Context, int) ([]receipt.Pending, error) { return []receipt.Pending{p, second}, nil }, commit: func(context.Context, receipt.Pending, receipt.Decision) (bool, error) { commits++; return true, nil }}, Authority: ownerFixture{resolve: func(_ context.Context, r receipt.OwnerReceipt, ref string) (receipt.Decision, error) {
		calls++
		if r.ExternalRef == "second" {
			return receipt.Decision{}, errs.Unavailable
		}
		if calls == 1 && ref != "" || calls == 2 && ref != d.Ref {
			t.Fatal("fresh explicit decision recheck missing")
		}
		return d, nil
	}}}
	if err := s.Cycle(t.Context()); err != nil || commits != 1 || calls != 3 || o["committed"] != 1 || o["error"] != 1 {
		t.Fatal("partial progress was lost")
	}
}

// TestWorkerBoundsAndNoDecision rejects invalid batch/interval limits and treats a missing owner decision as an observed no-op, never a commit.
// TestWorkerBoundsAndNoDecision отвергает неверные пределы пачки и интервала и учитывает отсутствие решения без действия, никогда не делая commit.
func TestWorkerBoundsAndNoDecision(t *testing.T) {
	p, _ := fixture()
	for _, limits := range []struct {
		batch    int
		interval time.Duration
	}{{0, 5 * time.Second}, {65, 5 * time.Second}, {1, time.Second}, {1, 301 * time.Second}} {
		s := Service{Batch: limits.batch, Interval: limits.interval, Repository: storeFixture{}, Authority: ownerFixture{}, Barrier: func(context.Context) error { return nil }}
		if err := s.Run(t.Context()); !errors.Is(err, errs.Invalid) {
			t.Fatal("unbounded worker configuration accepted")
		}
	}
	calls := 0
	o := observer{}
	s := Service{Batch: 1, Interval: 5 * time.Second, Observer: o, Repository: storeFixture{pending: func(context.Context, int) ([]receipt.Pending, error) { return []receipt.Pending{p}, nil }, commit: func(context.Context, receipt.Pending, receipt.Decision) (bool, error) {
		t.Fatal("no decision unlocked source")
		return false, nil
	}}, Authority: ownerFixture{resolve: func(context.Context, receipt.OwnerReceipt, string) (receipt.Decision, error) {
		calls++
		return receipt.Decision{}, errs.NotFound
	}}}
	if err := s.Cycle(t.Context()); err != nil || calls != 1 || o["none"] != 1 {
		t.Fatal("NOT_FOUND was not a bounded no-op")
	}
}
