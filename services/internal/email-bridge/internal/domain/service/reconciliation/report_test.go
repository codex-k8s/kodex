package reconciliation

import (
	"context"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
)

// reportStore substitutes only report-recovery paths and remembering, with no actual persistence or concurrency guarantees.
// reportStore подставляет только пути восстановления отчёта и запоминание, без реального хранения или гарантий конкуренции.
type reportStore struct {
	receipt.ReportRepository
	receipt.ReconciliationRepository
	pending  func(context.Context, int) ([]receipt.PendingReport, error)
	ack      func(context.Context, receipt.PendingReport) error
	remember func(context.Context) error
}

// PendingReports delegates the context and batch bound to a test-controlled journal read.
// PendingReports передаёт контекст и размер пачки тестовому чтению журнала.
func (s reportStore) PendingReports(ctx context.Context, batch int) ([]receipt.PendingReport, error) {
	return s.pending(ctx, batch)
}

// ClaimReport grants every synthetic claim, so worker tests do not stand in for PostgreSQL fencing tests.
// ClaimReport выдаёт каждый синтетический claim, поэтому тесты worker не заменяют проверку ограничений в PostgreSQL.
func (s reportStore) ClaimReport(context.Context, receipt.PendingReport, time.Duration) (bool, error) {
	return true, nil
}

// AcknowledgeReport exposes the reported version and context to the case-specific acknowledgement assertion.
// AcknowledgeReport передаёт версию отчёта и контекст проверке подтверждения конкретного сценария.
func (s reportStore) AcknowledgeReport(ctx context.Context, p receipt.PendingReport) error {
	return s.ack(ctx, p)
}

// Remember injects success, failure or a cancellable wait; it deliberately does not validate receipt fields itself.
// Remember внедряет успех, отказ либо отменяемое ожидание; поля квитанции намеренно не проверяются самой заглушкой.
func (s reportStore) Remember(ctx context.Context, _ receipt.Scope, _ receipt.Record, _ receipt.OwnerReceipt, _ time.Time) error {
	return s.remember(ctx)
}

// reportAuthority implements only the synthetic report path, leaving other owner methods outside this fixture.
// reportAuthority реализует только синтетический путь отчёта, оставляя прочие методы владельца вне этой оснастки.
type reportAuthority struct {
	receipt.EffectAuthority
	report func(context.Context, receipt.Report) (receipt.OwnerReceipt, error)
}

// Report delegates the original replay/source/receipt envelope to the controlled owner response.
// Report передаёт исходную оболочку replay/source/receipt управляемому ответу владельца.
func (a reportAuthority) Report(ctx context.Context, p receipt.Report) (receipt.OwnerReceipt, error) {
	return a.report(ctx, p)
}

// recoveryFixture builds a versioned report with expired synthetic invocation lease so recovery is eligible without waiting.
// recoveryFixture создаёт версионированный отчёт с истёкшим синтетическим lease invocation для восстановления без ожидания.
func recoveryFixture() receipt.PendingReport {
	p, _ := fixture()
	p.Record.ReportVersion = 1
	ref := p.Owner.Invocation
	return receipt.PendingReport{Scope: p.Scope, Record: p.Record, Source: receipt.ReportSource{Connection: p.Owner.Connection,
		Binding: &api.ExecutionBinding{InvocationRef: &ref, Lease: api.ExecutionLease{Ref: "lease_fixture01", Fence: "fixture-fence", Generation: 1, ExpiresAt: time.Now().Add(-time.Minute)}}}}
}

// TestReportWorkerStartupBarrierAndCancelJoin exercises cancellation at barrier, journal read, owner report, remember and acknowledgement checkpoints.
// Every injected dependency cooperates with context cancellation; the test waits for RunReports to return before dependency shutdown.
// TestReportWorkerStartupBarrierAndCancelJoin проверяет отмену на barrier, чтении журнала, отчёте владельцу, remember и подтверждении.
// Каждая заглушка соблюдает отмену контекста; тест ожидает возврат RunReports до закрытия зависимостей.
func TestReportWorkerStartupBarrierAndCancelJoin(t *testing.T) {
	for _, phase := range []string{"barrier", "pending", "rpc", "remember", "ack"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered := make(chan struct{})
			done := make(chan error, 1)
			block := func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
			p := recoveryFixture()
			store := reportStore{pending: func(ctx context.Context, _ int) ([]receipt.PendingReport, error) {
				if phase == "barrier" {
					t.Error("report polled before startup barrier")
				}
				if phase == "pending" {
					return nil, block(ctx)
				}
				return []receipt.PendingReport{p}, nil
			}, remember: func(ctx context.Context) error {
				if phase == "remember" {
					return block(ctx)
				}
				return nil
			}, ack: func(ctx context.Context, _ receipt.PendingReport) error { return block(ctx) }}
			s := Service{Repository: store, Reports: store, Interval: 5 * time.Second, Batch: 1, Barrier: func(ctx context.Context) error {
				if phase == "barrier" {
					return block(ctx)
				}
				return nil
			}, Authority: reportAuthority{report: func(ctx context.Context, r receipt.Report) (receipt.OwnerReceipt, error) {
				if phase == "rpc" {
					return receipt.OwnerReceipt{}, block(ctx)
				}
				r.Receipt.Ref, r.Receipt.Version = "receipt_fixture01", 1
				return r.Receipt, nil
			}}}
			go func() { done <- s.RunReports(ctx) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("report worker did not enter expected phase")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("report worker outlived dependency shutdown")
			}
		})
	}
}

// TestReportWorkerValidatesReadbackAndPreservesPartialProgress rejects changed lineage, denied reports and failed remembering before acknowledgement.
// A successful first item remains counted even when the second report fails; all requests must retain Replay=true.
// TestReportWorkerValidatesReadbackAndPreservesPartialProgress отвергает иное происхождение, запрет отчёта и отказ remember до подтверждения.
// Успешный первый элемент остаётся учтённым при сбое второго; все запросы должны сохранять Replay=true.
func TestReportWorkerValidatesReadbackAndPreservesPartialProgress(t *testing.T) {
	for _, mode := range []string{"success", "changed", "denied", "remember"} {
		t.Run(mode, func(t *testing.T) {
			p := recoveryFixture()
			second := p
			second.Record.ID = "second"
			acks := 0
			observed := observer{}
			store := reportStore{pending: func(context.Context, int) ([]receipt.PendingReport, error) {
				return []receipt.PendingReport{p, second}, nil
			},
				remember: func(context.Context) error {
					if mode == "remember" {
						return errs.Unavailable
					}
					return nil
				},
				ack: func(context.Context, receipt.PendingReport) error { acks++; return nil }}
			s := Service{Repository: store, Reports: store, Batch: 2, Interval: 5 * time.Second, Observer: observed, Authority: reportAuthority{report: func(_ context.Context, r receipt.Report) (receipt.OwnerReceipt, error) {
				if !r.Replay {
					t.Fatal("worker attempted live report")
				}
				if r.Receipt.ExternalRef == "second" {
					return receipt.OwnerReceipt{}, errs.Unavailable
				}
				if mode == "denied" {
					return receipt.OwnerReceipt{}, errs.Denied
				}
				r.Receipt.Ref, r.Receipt.Version = "receipt_fixture01", 1
				if mode == "changed" {
					r.Receipt.Invocation = "other"
				}
				return r.Receipt, nil
			}}}
			if err := s.ReportCycle(t.Context()); err != nil {
				t.Fatal(err)
			}
			want := 0
			if mode == "success" {
				want = 1
			}
			if acks != want || observed["reported"] != want || observed["error"] < 1 {
				t.Fatal("report result lost progress or accepted invalid acknowledgement")
			}
		})
	}
}
