package reconciliation

import (
	"context"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
)

type reportStore struct {
	receipt.ReportRepository
	receipt.ReconciliationRepository
	pending  func(context.Context, int) ([]receipt.PendingReport, error)
	ack      func(context.Context, receipt.PendingReport) error
	remember func(context.Context) error
}

func (s reportStore) PendingReports(ctx context.Context, batch int) ([]receipt.PendingReport, error) {
	return s.pending(ctx, batch)
}
func (s reportStore) ClaimReport(context.Context, receipt.PendingReport, time.Duration) (bool, error) {
	return true, nil
}
func (s reportStore) AcknowledgeReport(ctx context.Context, p receipt.PendingReport) error {
	return s.ack(ctx, p)
}
func (s reportStore) Remember(ctx context.Context, _ receipt.Scope, _ receipt.Record, _ receipt.OwnerReceipt, _ time.Time) error {
	return s.remember(ctx)
}

type reportAuthority struct {
	receipt.EffectAuthority
	report func(context.Context, receipt.Report) (receipt.OwnerReceipt, error)
}

func (a reportAuthority) Report(ctx context.Context, p receipt.Report) (receipt.OwnerReceipt, error) {
	return a.report(ctx, p)
}

func recoveryFixture() receipt.PendingReport {
	p, _ := fixture()
	p.Record.ReportVersion = 1
	ref := p.Owner.Invocation
	return receipt.PendingReport{Scope: p.Scope, Record: p.Record, Source: receipt.ReportSource{Connection: p.Owner.Connection,
		Binding: &api.ExecutionBinding{InvocationRef: &ref, Lease: api.ExecutionLease{Ref: "lease_fixture01", Fence: "fixture-fence", Generation: 1, ExpiresAt: time.Now().Add(-time.Minute)}}}}
}

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
