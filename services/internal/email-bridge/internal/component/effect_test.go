package component

import (
	"context"
	"errors"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
)

type effectFixture struct {
	report func(context.Context, port.Report) (port.OwnerReceipt, error)
}

func (f effectFixture) Report(ctx context.Context, r port.Report) (port.OwnerReceipt, error) {
	if f.report != nil {
		return f.report(ctx, r)
	}
	r.Receipt.Ref, r.Receipt.Version = "receipt_fixture01", 1
	return r.Receipt, nil
}
func (effectFixture) Reconcile(context.Context, port.OwnerReceipt, string) (port.Decision, error) {
	return port.Decision{}, errs.Denied
}

func TestDurableUnknownBeforeCPAndProvider(t *testing.T) {
	testDurableUnknown(t, nil)
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
