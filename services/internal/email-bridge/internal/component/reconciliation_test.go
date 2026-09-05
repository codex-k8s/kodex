package component

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/authority"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/reconciliation"
	repository "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/repository/postgres/receipt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func pendingFixture(t *testing.T, store *repository.Repository, key string, after time.Time) port.Pending {
	t.Helper()
	scope := port.Scope{Tenant: "tenant", Mailbox: "mailbox"}
	input := api.Digest(key)
	audit := port.Audit{Actor: "actor", Agent: "agent", Grant: "grant", Operation: api.OperationMarkRead, ConfigurationRevision: 1, CredentialGeneration: 1, GateApproved: true}
	r, created, err := store.Reserve(t.Context(), scope, key, input, input[:32], api.Digest("resource-"+key), audit)
	if err != nil || !created {
		t.Fatalf("reserve fixture: %v", err)
	}
	r.UID, r.UIDValidity, r.Folder = "42", 7, "INBOX"
	if err := store.Complete(t.Context(), scope, r, "unknown"); err != nil {
		t.Fatal(err)
	}
	o := port.OwnerReceipt{Ref: "receipt_" + r.ID, Version: 1, Invocation: "invocation_" + r.ID, ExternalRef: r.ID, ExternalDigest: r.ExternalDigest(scope), InputDigest: r.Digest, EffectKey: r.Key, Mailbox: scope.Mailbox, Connection: "connection", ConfigurationRevision: 1, Outcome: port.Unknown}
	if err := store.Remember(t.Context(), scope, r, o, after); err != nil {
		t.Fatalf("remember fixture: %v", err)
	}
	return port.Pending{Scope: scope, Record: r, Owner: o}
}

func decisionFixture(p port.Pending) port.Decision {
	return port.Decision{Ref: "decision_" + p.Record.ID, Version: 1, Actor: "owner", Grant: "fresh-grant", Receipt: p.Owner, Outcome: port.NoEffectConfirmed, ExpiresAt: time.Now().Add(time.Minute)}
}

func TestPostgresReconciliationAtomicLifecycle(t *testing.T) {
	store := postgresFixture(t)
	p := pendingFixture(t, store, "reconcile", time.Now().Add(-time.Minute))
	if _, _, err := store.Reserve(t.Context(), p.Scope, "other", p.Record.Digest, "other-id", p.Record.Resource, p.Record.Audit); !errors.Is(err, errs.Conflict) {
		t.Fatal("source was not locked")
	}
	rows, err := store.Pending(t.Context(), 1)
	if err != nil || len(rows) != 1 || !rows[0].Valid() {
		t.Fatalf("pending selection: %v", err)
	}
	if ok, err := store.ClaimReconciliation(t.Context(), p, 5*time.Second); err != nil || !ok {
		t.Fatal("first claim failed")
	}
	if ok, err := store.ClaimReconciliation(t.Context(), p, 5*time.Second); err != nil || ok {
		t.Fatal("parallel claim accepted")
	}
	d := decisionFixture(p)
	var changed atomic.Int32
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			ok, err := store.CommitReconciliation(t.Context(), p, d)
			if err != nil {
				t.Errorf("concurrent commit: %v", err)
			}
			if ok {
				changed.Add(1)
			}
		}()
	}
	group.Wait()
	if changed.Load() != 1 {
		t.Fatal("reconciliation was not idempotent")
	}
	if err := store.Complete(t.Context(), p.Scope, p.Record, "accepted"); err == nil {
		t.Fatal("late completion overwrote reconciled UNKNOWN")
	}
	original, err := store.Get(t.Context(), p.Scope, p.Record.ID, "")
	if err != nil || original.Status != "unknown" || original.UID != "42" || original.UIDValidity != 7 || original.Folder != "INBOX" || original.ExternalDigest(p.Scope) != p.Owner.ExternalDigest {
		t.Fatal("original UNKNOWN receipt was rewritten")
	}
	var auditCount int
	if err := store.Pool.QueryRow(t.Context(), "SELECT count(*) FROM email_bridge.owner_receipts WHERE decision_ref=$1 AND decision_grant=$2 AND decision_outcome='NO_EFFECT_CONFIRMED' AND outcome='UNKNOWN_OUTCOME'", d.Ref, d.Grant).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal("durable decision audit missing")
	}
	if _, created, err := store.Reserve(t.Context(), p.Scope, "other", p.Record.Digest, "other-id", p.Record.Resource, p.Record.Audit); err != nil || !created {
		t.Fatalf("authorized source unlock failed: %v", err)
	}
	if rows, err := store.Pending(t.Context(), 64); err != nil || len(rows) != 0 {
		t.Fatal("reconciled receipt still pending")
	}
	for _, change := range []func(*port.Decision){func(d *port.Decision) { d.Grant = "other" }, func(d *port.Decision) { d.Ref = "other-decision" }, func(d *port.Decision) { d.Outcome = port.EffectConfirmed }} {
		copy := d
		change(&copy)
		if _, err := store.CommitReconciliation(t.Context(), p, copy); err == nil {
			t.Fatal("conflicting decision accepted")
		}
	}
}

func TestPostgresReconciliationExpiryAndBinding(t *testing.T) {
	store := postgresFixture(t)
	p := pendingFixture(t, store, "expiry", time.Now().Add(-time.Minute))
	for _, mutate := range []func(*port.Pending, *port.Decision){
		func(_ *port.Pending, d *port.Decision) { d.ExpiresAt = time.Now().Add(-time.Second) },
		func(_ *port.Pending, d *port.Decision) { d.Outcome = port.Unknown },
		func(_ *port.Pending, d *port.Decision) { d.Receipt.ExternalDigest = strings.Repeat("f", 64) },
		func(_ *port.Pending, d *port.Decision) { d.Receipt.Version++ },
		func(p *port.Pending, _ *port.Decision) { p.Scope.Tenant = "other" },
		func(_ *port.Pending, d *port.Decision) { d.Grant = "" },
	} {
		copy, d := p, decisionFixture(p)
		mutate(&copy, &d)
		if _, err := store.CommitReconciliation(t.Context(), copy, d); err == nil {
			t.Fatal("invalid authority accepted")
		}
	}
	tx, err := store.Pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err := tx.Exec(t.Context(), "SELECT message_id FROM email_bridge.owner_receipts WHERE message_id=$1 FOR UPDATE", p.Record.ID); err != nil {
		t.Fatal(err)
	}
	d := decisionFixture(p)
	d.ExpiresAt = time.Now().Add(300 * time.Millisecond)
	done := make(chan error, 1)
	go func() { _, err := store.CommitReconciliation(t.Context(), p, d); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("locked commit ignored deadline")
		}
	case <-time.After(time.Second):
		t.Fatal("commit deadline not bounded")
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	var unlocked bool
	if err := store.Pool.QueryRow(t.Context(), "SELECT source_unlocked FROM email_bridge.receipts WHERE message_id=$1", p.Record.ID).Scan(&unlocked); err != nil || unlocked {
		t.Fatal("expired transaction unlocked source")
	}
	if _, _, err := store.Reserve(t.Context(), p.Scope, "still-locked", p.Record.Digest, "still-locked-id", p.Record.Resource, p.Record.Audit); !errors.Is(err, errs.Conflict) {
		t.Fatal("expired decision released source")
	}
}

type reconciliationRPC struct {
	cp.RuntimeWorkServiceClient
	resolve func(context.Context, *cp.ResolveEmailReconciliationRequest) (*cp.ResolveEmailReconciliationResponse, error)
}

func (f reconciliationRPC) ResolveEmailReconciliation(ctx context.Context, r *cp.ResolveEmailReconciliationRequest, _ ...grpc.CallOption) (*cp.ResolveEmailReconciliationResponse, error) {
	return f.resolve(ctx, r)
}

func reconciliationResponse(d port.Decision) *cp.ResolveEmailReconciliationResponse {
	o := d.Receipt
	return &cp.ResolveEmailReconciliationResponse{Receipt: &cp.EmailEffectReceipt{Ref: o.Ref, Version: o.Version, InvocationRef: o.Invocation, ExternalReceiptRef: o.ExternalRef, ExternalReceiptDigest: o.ExternalDigest, SemanticInputDigest: o.InputDigest, EffectKey: o.EffectKey, MailboxRef: o.Mailbox, ConnectionRef: o.Connection, ConfigurationRevision: o.ConfigurationRevision, Outcome: cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME}, Decision: &cp.EmailReconciliationDecision{Ref: d.Ref, Version: d.Version, ReceiptRef: o.Ref, ReceiptVersion: o.Version, ReceiptDigest: o.ExternalDigest, InvocationRef: o.Invocation, ActorRef: d.Actor, GrantRef: d.Grant, Outcome: cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED, CreatedAt: timestamppb.New(time.Now().Add(-time.Minute)), ExpiresAt: timestamppb.New(d.ExpiresAt)}}
}

func TestPostgresReconciliationFakeCPConsumer(t *testing.T) {
	for _, scenario := range []string{"success", "no-decision", "revoked", "changed", "expired", "digest", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			store := postgresFixture(t)
			p := pendingFixture(t, store, scenario, time.Now().Add(-time.Minute))
			d := decisionFixture(p)
			calls := 0
			client := &authority.Client{API: reconciliationRPC{resolve: func(ctx context.Context, r *cp.ResolveEmailReconciliationRequest) (*cp.ResolveEmailReconciliationResponse, error) {
				calls++
				if r.ReceiptRef != p.Owner.Ref || r.ExternalReceiptRef != p.Record.ID || r.ExternalReceiptDigest != p.Owner.ExternalDigest || calls == 1 && r.DecisionRef != "" || calls == 2 && r.DecisionRef != d.Ref {
					t.Fatal("owner lookup lost exact binding")
				}
				if scenario == "no-decision" {
					return nil, status.Error(codes.NotFound, "no decision")
				}
				if scenario == "unavailable" {
					return nil, status.Error(codes.Unavailable, "fixture unavailable")
				}
				if scenario == "revoked" && calls == 2 {
					return nil, status.Error(codes.PermissionDenied, "revoked")
				}
				response := reconciliationResponse(d)
				if scenario == "changed" && calls == 2 {
					response.Decision.GrantRef = "other-grant"
				}
				if scenario == "expired" && calls == 2 {
					response.Decision.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
				}
				if scenario == "digest" {
					response.Receipt.ExternalReceiptDigest = strings.Repeat("f", 64)
				}
				return response, nil
			}}}
			worker := &reconciliation.Service{Repository: store, Authority: client, Batch: 2, Interval: 5 * time.Second}
			if err := worker.Cycle(t.Context()); err != nil {
				t.Fatal(err)
			}
			var unlocked bool
			if err := store.Pool.QueryRow(t.Context(), "SELECT source_unlocked FROM email_bridge.receipts WHERE message_id=$1", p.Record.ID).Scan(&unlocked); err != nil || unlocked != (scenario == "success") {
				t.Fatal("wrong reconciliation result")
			}
			if calls < 1 || calls > 2 {
				t.Fatal("unbounded owner calls")
			}
			before := calls
			if err := worker.Cycle(t.Context()); err != nil || calls != before {
				t.Fatal("poll interval was bypassed")
			}
		})
	}
}

func TestPostgresReconciliationBoundedSelection(t *testing.T) {
	store := postgresFixture(t)
	for _, key := range []string{"first", "second", "third"} {
		pendingFixture(t, store, key, time.Now().Add(-time.Minute))
	}
	pendingFixture(t, store, "inflight", time.Now().Add(time.Minute))
	audit := port.Audit{Actor: "actor", Agent: "agent", Grant: "grant", Operation: api.OperationMarkRead, ConfigurationRevision: 1, CredentialGeneration: 1}
	if _, created, err := store.Reserve(t.Context(), port.Scope{Tenant: "tenant", Mailbox: "mailbox"}, "unlinked", api.Digest("unlinked"), "unlinked-id", "", audit); err != nil || !created {
		t.Fatal("unlinked receipt fixture failed")
	}
	rows, err := store.Pending(t.Context(), 2)
	if err != nil || len(rows) != 2 {
		t.Fatal("batch bound failed")
	}
	for _, p := range rows {
		if ok, err := store.ClaimReconciliation(t.Context(), p, 5*time.Second); err != nil || !ok {
			t.Fatal("claim failed")
		}
	}
	remaining, err := store.Pending(t.Context(), 2)
	if err != nil || len(remaining) != 1 || remaining[0].Record.Key == "inflight" {
		t.Fatal("fair scheduling or inflight barrier failed")
	}
	if _, err := store.Pending(t.Context(), 65); err == nil {
		t.Fatal("unbounded batch accepted")
	}
}

func TestPostgresOwnerAcknowledgementMonotonic(t *testing.T) {
	store := postgresFixture(t)
	p := pendingFixture(t, store, "monotonic", time.Now().Add(-time.Minute))
	known := p.Record
	known.Status = "accepted"
	if err := store.Complete(t.Context(), p.Scope, known, "accepted"); err != nil {
		t.Fatal(err)
	}
	owner := p.Owner
	owner.Version = 2
	owner.Outcome = port.EffectConfirmed
	if err := store.Remember(t.Context(), p.Scope, known, owner, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Remember(t.Context(), p.Scope, p.Record, p.Owner, time.Now()); err != nil {
		t.Fatal("late exact UNKNOWN acknowledgement was not a no-op")
	}
	var version int64
	var outcome string
	if err := store.Pool.QueryRow(t.Context(), "SELECT owner_version,outcome FROM email_bridge.owner_receipts WHERE message_id=$1", p.Record.ID).Scan(&version, &outcome); err != nil || version != 2 || outcome != string(port.EffectConfirmed) {
		t.Fatal("owner acknowledgement regressed")
	}
	for _, v := range []int64{2, 3} {
		conflict := p.Owner
		conflict.Version = v
		if err := store.Remember(t.Context(), p.Scope, p.Record, conflict, time.Now()); !errors.Is(err, errs.Conflict) {
			t.Fatal("terminal acknowledgement was replaced by UNKNOWN")
		}
	}
	conflict := p.Owner
	conflict.Ref = "receipt_other"
	if err := store.Remember(t.Context(), p.Scope, p.Record, conflict, time.Now()); !errors.Is(err, errs.Conflict) {
		t.Fatal("owner identity replacement accepted")
	}
}

func TestPostgresReconciliationCorruptSnapshot(t *testing.T) {
	store := postgresFixture(t)
	p := pendingFixture(t, store, "corrupt", time.Now().Add(-time.Minute))
	if _, err := store.Pool.Exec(t.Context(), "UPDATE email_bridge.receipts SET grant_id='other-grant' WHERE message_id=$1", p.Record.ID); err != nil {
		t.Fatal(err)
	}
	client := &authority.Client{API: reconciliationRPC{resolve: func(context.Context, *cp.ResolveEmailReconciliationRequest) (*cp.ResolveEmailReconciliationResponse, error) {
		t.Fatal("corrupt receipt reached CP")
		return nil, nil
	}}}
	worker := &reconciliation.Service{Repository: store, Authority: client, Batch: 1, Interval: 5 * time.Second}
	if err := worker.Cycle(t.Context()); err != nil {
		t.Fatal(err)
	}
	var unlocked bool
	if err := store.Pool.QueryRow(t.Context(), "SELECT source_unlocked FROM email_bridge.receipts WHERE message_id=$1", p.Record.ID).Scan(&unlocked); err != nil || unlocked {
		t.Fatal("corrupt snapshot unlocked source")
	}
}

func TestPostgresReconciliationConfirmedEffect(t *testing.T) {
	store := postgresFixture(t)
	p := pendingFixture(t, store, "confirmed-effect", time.Now().Add(-time.Minute))
	d := decisionFixture(p)
	d.Outcome = port.EffectConfirmed
	if changed, err := store.CommitReconciliation(t.Context(), p, d); err != nil || !changed {
		t.Fatalf("confirmed effect commit failed: %v", err)
	}
	r, err := store.Get(t.Context(), p.Scope, p.Record.ID, "")
	if err != nil || r.Status != "unknown" {
		t.Fatal("confirmed decision rewrote source outcome")
	}
	if _, created, err := store.Reserve(t.Context(), p.Scope, "new-intention", p.Record.Digest, "new-id", p.Record.Resource, p.Record.Audit); err != nil || !created {
		t.Fatal("confirmed decision did not release source")
	}
}
