package component

import (
	"context"
	"errors"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
)

type effectFixture struct {
	report func(context.Context, port.Report) (port.OwnerReceipt, error)
}

func (f effectFixture) Report(ctx context.Context, r port.Report) (port.OwnerReceipt, error) {
	result := r.Receipt
	if f.report != nil {
		var err error
		result, err = f.report(ctx, r)
		if err != nil {
			return result, err
		}
	}
	result.Ref, result.Version = "receipt_"+r.Receipt.ExternalRef, 1
	if result.Outcome != port.Unknown {
		result.Version = 2
	}
	if result.Invocation == "" {
		result.Invocation = "inv_fixture01"
	}
	return result, nil
}
func (effectFixture) Reconcile(context.Context, port.OwnerReceipt, string) (port.Decision, error) {
	return port.Decision{}, errs.Denied
}

func TestDurableUnknownBeforeCPAndProvider(t *testing.T) {
	testDurableUnknown(t, nil)
}

type failingLedger struct{ port.ReconciliationRepository }

func (failingLedger) Remember(context.Context, port.Scope, port.Record, port.OwnerReceipt, time.Time) error {
	return errs.Unavailable
}

func TestOwnerBindingMustPersistBeforeProvider(t *testing.T) {
	f := newFixture(t, "implicit")
	s, sec, _ := service(t, f, "implicit", nil)
	s.Ledger = failingLedger{}
	if _, err := s.Execute(t.Context(), httptransport.CallerSPIFFE, "fixture", send(api.OperationSend, "journal-failure")); !errors.Is(err, errs.Unavailable) || sec.reads.Load() != 0 {
		t.Fatal("provider started without durable CP identity")
	}
}

func TestEmptyMailboxConfigurationFailsClosed(t *testing.T) {
	f := newFixture(t, "implicit")
	s, sec, _ := service(t, f, "implicit", nil)
	s.Config.Mailboxes = nil
	for _, op := range []api.Operation{api.OperationHealth, api.OperationSend} {
		if _, err := s.Execute(t.Context(), httptransport.CallerSPIFFE, "fixture", send(op, "not-configured")); !errors.Is(err, errs.NotFound) || sec.reads.Load() != 0 {
			t.Fatal("unconfigured mailbox reached provider")
		}
	}
}

func TestPostgresDurableUnknownBeforeCPAndProvider(t *testing.T) {
	testDurableUnknown(t, postgresFixture(t))
}

func testDurableUnknown(t *testing.T, store port.Repository) {
	t.Helper()
	for _, fail := range []string{"none", "unknown-report", "terminal-report", "missing-client"} {
		t.Run(fail, func(t *testing.T) {
			f := newFixture(t, "implicit")
			s, sec, _ := service(t, f, "implicit", store)
			if store != nil {
				s.Config = receiptConfiguration()
			}
			command := send(api.OperationSend, "report-order-"+fail)
			scope := port.Scope{Tenant: "tenant", Mailbox: "mailbox"}
			calls := 0
			var digest string
			s.Effects = effectFixture{report: func(ctx context.Context, request port.Report) (port.OwnerReceipt, error) {
				calls++
				r, err := s.Receipts.Get(ctx, scope, request.Receipt.ExternalRef, "")
				if err != nil || r.ExternalDigest(scope) != request.Receipt.ExternalDigest {
					t.Fatal("report preceded durable receipt")
				}
				if calls == 1 {
					digest = request.Receipt.ExternalDigest
					if r.Status != "unknown" || request.Receipt.Outcome != port.Unknown || sec.reads.Load() != 0 {
						t.Fatal("provider preceded durable UNKNOWN acknowledgement")
					}
				} else if digest != request.Receipt.ExternalDigest {
					t.Fatal("receipt identity changed with outcome")
				}
				if fail == "unknown-report" || fail == "terminal-report" && request.Receipt.Outcome != port.Unknown {
					return port.OwnerReceipt{}, errs.Unavailable
				}
				return request.Receipt, nil
			}}
			if fail == "missing-client" {
				s.Effects = nil
			}
			result, err := s.Execute(t.Context(), httptransport.CallerSPIFFE, "fixture-token", command)
			if fail == "none" && (err != nil || result.Status != "accepted" || calls != 2) {
				t.Fatalf("report lifecycle failed: %v", err)
			}
			if fail != "none" && !errors.Is(err, errs.Unavailable) {
				t.Fatalf("expected closed refusal, got %v", err)
			}
			if (fail == "unknown-report" || fail == "missing-client") && sec.reads.Load() != 0 {
				t.Fatal("failed CP allowed credential access")
			}
			before := sec.reads.Load()
			if fail == "terminal-report" {
				// Восстановление CP допубликовывает receipt, но не повторяет SMTP.
				s.Effects = effectFixture{}
				replay, err := s.Execute(t.Context(), httptransport.CallerSPIFFE, "fixture-token", command)
				if err != nil || replay.Status != "accepted" || sec.reads.Load() != before {
					t.Fatal("receipt recovery repeated provider effect")
				}
			}
		})
	}
}
