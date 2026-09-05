package component

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/reconciliation"
	repository "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/repository/postgres/receipt"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
)

func reportFixture(t *testing.T, store *repository.Repository, key string) port.PendingReport {
	t.Helper()
	digest := api.Digest(key)
	p := port.PendingReport{Scope: port.Scope{Tenant: "tenant", Mailbox: "mailbox"},
		Record: port.Record{ID: digest[:32], Key: key, Digest: digest, Resource: api.Digest("resource-" + key),
			Audit: port.Audit{Actor: "actor", Agent: "agent", Grant: "grant", Operation: api.OperationMarkRead, ConfigurationRevision: 1, CredentialGeneration: 1}},
		Source: port.ReportSource{Binding: executionFixture(), Connection: "connection"}}
	p.Source.Binding.Lease.ExpiresAt = time.Now().Add(500 * time.Millisecond)
	r, created, err := store.ReserveEffect(t.Context(), p.Scope, p.Record, p.Source)
	if err != nil || !created || r.ReportVersion != 1 {
		t.Fatalf("atomic report reservation failed: %v", err)
	}
	p.Record = r
	return p
}

func awaitReport(t *testing.T, binding *api.ExecutionBinding) {
	t.Helper()
	timer := time.NewTimer(time.Until(binding.Lease.ExpiresAt.Add(port.ReportGrace + 10*time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal("report recovery fixture cancelled")
	}
}

func TestPostgresReportLostResponseRecovery(t *testing.T) {
	for _, phase := range []string{"initial", "terminal"} {
		t.Run(phase, func(t *testing.T) {
			store := postgresFixture(t)
			f := newFixture(t, "implicit")
			s, credentials, _ := service(t, f, "implicit", store)
			s.Config = receiptConfiguration()
			binding := executionFixture()
			binding.Lease.ExpiresAt = time.Now().Add(time.Second)
			var original port.Report
			var calls int
			s.Effects = effectFixture{report: func(_ context.Context, request port.Report) (port.OwnerReceipt, error) {
				calls++
				if phase == "initial" || request.Receipt.Outcome != port.Unknown {
					original = request
					return port.OwnerReceipt{}, errs.Unavailable
				}
				return request.Receipt, nil
			}}
			command := send(api.OperationSend, "lost-response-"+phase)
			_, err := s.Execute(api.WithExecutionBinding(t.Context(), binding), httptransport.CallerSPIFFE, binding.Lease.Fence, command)
			if !errors.Is(err, errs.Unavailable) || original.Binding == nil || phase == "initial" && credentials.reads.Load() != 0 {
				t.Fatal("lost response did not preserve pre-provider boundary")
			}
			before := credentials.reads.Load()
			if phase == "terminal" && before == 0 {
				t.Fatal("terminal fixture never reached provider")
			}
			awaitReport(t, binding)
			// Новый adapter читает только устойчивый журнал, без контекста HTTP-запроса.
			restarted := &repository.Repository{Pool: store.Pool}
			replays := 0
			worker := reconciliation.Service{Repository: restarted, Reports: restarted, Batch: 8, Interval: 5 * time.Second,
				Authority: effectFixture{report: func(_ context.Context, request port.Report) (port.OwnerReceipt, error) {
					replays++
					if !request.Replay || api.Digest(request.Binding) != api.Digest(original.Binding) || request.IdempotencyKey != original.IdempotencyKey || request.Receipt != original.Receipt {
						t.Fatal("recovery changed original report identity")
					}
					return original.Receipt, nil
				}}}
			if err := worker.ReportCycle(t.Context()); err != nil || replays != 1 {
				t.Fatalf("durable report replay failed: %v", err)
			}
			if err := worker.ReportCycle(t.Context()); err != nil || replays != 1 {
				t.Fatal("acknowledged report repeated")
			}
			var pending, sourcePresent bool
			if err := store.Pool.QueryRow(t.Context(), "SELECT report_pending,report_source IS NOT NULL FROM email_bridge.receipts WHERE message_id=$1", original.Receipt.ExternalRef).Scan(&pending, &sourcePresent); err != nil || pending || sourcePresent {
				t.Fatal("acknowledgement did not retire private execution binding")
			}
			var outcome string
			if err := store.Pool.QueryRow(t.Context(), "SELECT outcome FROM email_bridge.owner_receipts WHERE message_id=$1", original.Receipt.ExternalRef).Scan(&outcome); err != nil || outcome != string(original.Receipt.Outcome) {
				t.Fatal("recovered owner identity missing")
			}
			fresh := executionFixture()
			if _, err := s.Execute(api.WithExecutionBinding(t.Context(), fresh), httptransport.CallerSPIFFE, fresh.Lease.Fence, command); err != nil || credentials.reads.Load() != before || replays != 1 {
				t.Fatal("client retry repeated provider effect")
			}
			if calls != map[string]int{"initial": 1, "terminal": 2}[phase] {
				t.Fatal("client retry substituted a fresh invocation in report")
			}
		})
	}
}

func TestPostgresReportGenerationAndClaim(t *testing.T) {
	store := postgresFixture(t)
	p := reportFixture(t, store, "generation")
	if rows, err := store.PendingReports(t.Context(), 1); err != nil || len(rows) != 0 {
		t.Fatal("live provider execution entered recovery")
	}
	if err := store.AcknowledgeReport(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	completed, err := store.CompleteEffect(t.Context(), p.Scope, p.Record, "accepted", p.Source)
	if err != nil || completed.ReportVersion != 2 {
		t.Fatal("terminal receipt and report were not committed together")
	}
	if err := store.AcknowledgeReport(t.Context(), p); !errors.Is(err, errs.Conflict) {
		t.Fatal("stale acknowledgement cleared terminal report")
	}
	if _, err := store.CompleteEffect(t.Context(), p.Scope, p.Record, "failed", p.Source); err == nil {
		t.Fatal("stale completion overwrote result")
	}
	p.Record = completed
	awaitReport(t, p.Source.Binding)
	rows, err := store.PendingReports(t.Context(), 1)
	if err != nil || len(rows) != 1 || rows[0].Record.ReportVersion != 2 {
		t.Fatalf("terminal journal unavailable: %v", err)
	}
	var claimed atomic.Int32
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			ok, err := store.ClaimReport(t.Context(), p, 5*time.Second)
			if err != nil {
				t.Error("report claim failed")
			}
			if ok {
				claimed.Add(1)
			}
		})
	}
	group.Wait()
	if claimed.Load() != 1 {
		t.Fatal("parallel workers claimed the same journal generation")
	}
	wrong := p
	wrong.Scope.Tenant = "other"
	if err := store.AcknowledgeReport(t.Context(), wrong); !errors.Is(err, errs.Conflict) {
		t.Fatal("cross-tenant acknowledgement accepted")
	}
	if err := store.AcknowledgeReport(t.Context(), p); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresReportAtomicValidationAndRetirement(t *testing.T) {
	store := postgresFixture(t)
	p := reportFixture(t, store, "validation")
	bad := p.Source
	bad.Connection = "other"
	if _, err := store.CompleteEffect(t.Context(), p.Scope, p.Record, "accepted", bad); err == nil {
		t.Fatal("changed source replaced original binding")
	}
	bad.Binding = nil
	other := p.Record
	other.Key, other.ID = "invalid", api.Digest("invalid")[:32]
	if _, _, err := store.ReserveEffect(t.Context(), p.Scope, other, bad); !errors.Is(err, errs.Invalid) {
		t.Fatal("invalid source reserved receipt")
	}
	if _, err := store.Get(t.Context(), p.Scope, other.ID, ""); !errors.Is(err, errs.NotFound) {
		t.Fatal("failed validation left a receipt without recovery journal")
	}
	if err := store.AcknowledgeReport(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	awaitReport(t, p.Source.Binding)
	if _, err := store.CompleteEffect(t.Context(), p.Scope, p.Record, "accepted", p.Source); err == nil {
		t.Fatal("expired live completion accepted")
	}
	if rows, err := store.PendingReports(t.Context(), 1); err != nil || len(rows) != 0 {
		t.Fatal("acknowledged reservation remained pending")
	}
	var retired bool
	if err := store.Pool.QueryRow(t.Context(), "SELECT report_source IS NULL FROM email_bridge.receipts WHERE message_id=$1", p.Record.ID).Scan(&retired); err != nil || !retired {
		t.Fatal("abandoned acknowledged execution retained fence")
	}
}

func TestPostgresReportCorruptSourceFailsClosed(t *testing.T) {
	store := postgresFixture(t)
	p := reportFixture(t, store, "corruption")
	if _, err := store.Pool.Exec(t.Context(), "UPDATE email_bridge.receipts SET report_source=jsonb_set(report_source,'{connection}','\"other\"') WHERE message_id=$1", p.Record.ID); err != nil {
		t.Fatal(err)
	}
	awaitReport(t, p.Source.Binding)
	if _, err := store.PendingReports(t.Context(), 1); !errors.Is(err, errs.Unavailable) {
		t.Fatal("corrupt persisted report became authoritative")
	}
}
