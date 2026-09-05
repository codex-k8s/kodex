package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/email_receipt_fixture.sql
var emailReceiptFixtureQuery string

func testEmailReceiptReconciliation(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.email-effects.reconcile",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-receipt-project"}, Payload: command.ProjectInput{Name: "Email reconciliation", Purpose: "Receipt ownership", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("email project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "email-receipt-agent", "Email fixture")
	run, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-receipt-run"}, Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref,
			Title: "Email receipt", Task: "Reconcile one exact effect", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}})
	if err != nil || run.Run == nil {
		t.Fatalf("email run: %v", err)
	}
	if _, err := pool.Exec(ctx, emailReceiptFixtureQuery, project.Project.Ref, run.Run.Ref, agent.Ref); err != nil {
		t.Fatal(err)
	}
	view, err := service.GetEmailEffectReceipt(ctx, owner, "email_receipt_invocation")
	if err != nil || view.Receipt.Ref != "emrc_email_fixture" || view.Receipt.ProjectRef != project.Project.Ref || view.Decision != nil {
		t.Fatalf("email owner read: %v", err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "email-bridge", Operation: "platform.email.reconciliation.resolve",
	}, "email-bridge")
	if _, err := service.ResolveEmailReconciliation(ctx, worker, view.Receipt.Ref, "", view.Receipt.ExternalReceiptRef, view.Receipt.ExternalReceiptDigest); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("missing owner-selected decision: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO control_plane.email_reconciliation_decisions
        (ref,organization_id,receipt_id,receipt_version,receipt_digest,outcome,grant_ref,actor_id,note,created_at,expires_at)
        SELECT 'emrd_expired_fixture',e.organization_id,e.id,e.version,e.external_receipt_digest,'NO_EFFECT_CONFIRMED','emrg_expired_fixture',r.initiated_by,'',
               clock_timestamp()-interval '4 minutes',clock_timestamp()-interval '3 minutes'
        FROM control_plane.email_effect_receipts e JOIN control_plane.integration_invocations i ON i.id=e.invocation_id
        JOIN control_plane.runs r ON r.id=i.run_id WHERE e.ref='emrc_email_fixture'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveEmailReconciliation(ctx, worker, view.Receipt.Ref, "", view.Receipt.ExternalReceiptRef, view.Receipt.ExternalReceiptDigest); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("expired owner-selected decision: %v", err)
	}
	if _, err := service.ResolveEmailReconciliation(ctx, worker, view.Receipt.Ref, "emrd_expired_fixture", view.Receipt.ExternalReceiptRef, view.Receipt.ExternalReceiptDigest); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("expired explicit decision: %v", err)
	}
	version := int64(1)
	input := command.Command{Kind: command.ReconcileEmailEffect, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-reconcile", ExpectedVersion: &version},
		Payload:  command.EmailReconciliationInput{ReceiptRef: view.Receipt.Ref, ExpectedReceiptDigest: strings.Repeat("c", 64), Outcome: "NO_EFFECT_CONFIRMED", Note: strings.Repeat("\u044f", 2000)}}
	if _, err := service.Execute(ctx, input); !errors.Is(err, errs.ErrFreshAuthenticationRequired) {
		t.Fatalf("reconciliation without freshness: %v", err)
	}
	input.Principal.CredentialAuthenticatedAt = time.Now().Add(-time.Minute)
	input.Principal.CredentialACR = "urn:test:interactive"
	input.Principal.CredentialAMR = []string{"pwd"}
	for _, kind := range []string{"version", "digest", "outcome"} {
		wrong := input
		wrong.Mutation.IdempotencyKey = "email-reconcile-wrong-" + kind
		payload := wrong.Payload.(command.EmailReconciliationInput)
		want := errs.ErrConflict
		switch kind {
		case "version":
			otherVersion := int64(2)
			wrong.Mutation.ExpectedVersion = &otherVersion
			want = errs.ErrVersionMismatch
		case "digest":
			payload.ExpectedReceiptDigest = strings.Repeat("d", 64)
		case "outcome":
			payload.Outcome = "UNKNOWN_OUTCOME"
			want = errs.ErrInvalid
		}
		wrong.Payload = payload
		if _, err := service.Execute(ctx, wrong); !errors.Is(err, want) {
			t.Fatalf("reconciliation wrong %s: %v", kind, err)
		}
	}
	result, err := service.Execute(ctx, input)
	if err != nil || result.EmailDecision == nil || result.EmailDecision.ReceiptVersion != 1 || result.EmailDecision.GrantRef == "" ||
		result.EmailDecision.ExpiresAt.Sub(result.EmailDecision.CreatedAt) != 2*time.Minute {
		t.Fatalf("email reconciliation: %v", err)
	}
	replay, err := service.Execute(ctx, input)
	if err != nil || replay.EmailDecision == nil || replay.EmailDecision.Ref != result.EmailDecision.Ref {
		t.Fatalf("email replay: %v", err)
	}
	stale := input
	stale.Principal.CredentialAuthenticatedAt = time.Now().Add(-6 * time.Minute)
	if _, err := service.Execute(ctx, stale); !errors.Is(err, errs.ErrFreshAuthenticationRequired) {
		t.Fatalf("stale authentication replay: %v", err)
	}
	other := input
	other.Mutation.IdempotencyKey = "email-opposite-decision"
	payload := other.Payload.(command.EmailReconciliationInput)
	payload.Outcome = "EFFECT_CONFIRMED"
	other.Payload = payload
	if _, err := service.Execute(ctx, other); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("opposite reconciliation: %v", err)
	}
	view, err = service.GetEmailEffectReceipt(ctx, owner, "email_receipt_invocation")
	if err != nil || view.Receipt.Version != 1 || view.Receipt.Outcome != "UNKNOWN_OUTCOME" || view.Decision == nil {
		t.Fatalf("reconciliation overwrote receipt: %v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM control_plane.integration_invocations WHERE ref='email_receipt_invocation'`).Scan(&state); err != nil || state != "UNKNOWN_OUTCOME" {
		t.Fatalf("reconciliation scheduled automatic retry: %q %v", state, err)
	}
	foreign := owner
	foreign.ProjectRef = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	if _, err := service.GetEmailEffectReceipt(ctx, foreign, "email_receipt_invocation"); err == nil {
		t.Fatal("foreign project authority received email receipt")
	}
	candidate := input.Principal
	if err := pool.QueryRow(ctx, `INSERT INTO control_plane.subjects
        (organization_id,ref,issuer,external_subject_digest,display_name)
        VALUES($1::uuid,'usr_email_receipt_reader','component.test',repeat('e',64),'Email receipt reader') RETURNING id::text`,
		owner.AuthorityTenant).Scan(&candidate.ActorID); err != nil {
		t.Fatal(err)
	}
	runRole := createRoleImageAccessRole(t, ctx, service, owner, "email-run-role", "Email run reader", []string{"run.view"}, []string{"ORGANIZATION"})
	createRoleImageAccessBinding(t, ctx, service, owner, "email-run-binding", "usr_email_receipt_reader", runRole.CurrentVersion.Ref, entity.AccessScope{Kind: "ORGANIZATION"})
	if _, err := service.GetEmailEffectReceipt(ctx, candidate, "email_receipt_invocation"); err == nil {
		t.Fatal("run-only reader received connection receipt")
	}
	integrationRole := createRoleImageAccessRole(t, ctx, service, owner, "email-integration-role", "Email connection manager", []string{"integration.view", "integration.manage"}, []string{"ORGANIZATION"})
	binding, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-integration-binding"}, Payload: command.AccessBindingInput{
			SubjectKind: "USER", SubjectRef: "usr_email_receipt_reader", RoleVersionRef: integrationRole.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "ORGANIZATION"}}})
	if err != nil || binding.AccessBinding == nil {
		t.Fatalf("email manager binding: %v", err)
	}
	if _, err := service.GetEmailEffectReceipt(ctx, candidate, "email_receipt_invocation"); err != nil {
		t.Fatalf("intersection receipt read: %v", err)
	}
	candidateCommand := input
	candidateCommand.Principal = candidate
	candidateCommand.Mutation.IdempotencyKey = "email-manager-reconcile"
	if _, err := service.Execute(ctx, candidateCommand); err != nil {
		t.Fatalf("intersection reconciliation: %v", err)
	}
	latest, err := service.GetEmailEffectReceipt(ctx, owner, "email_receipt_invocation")
	if err != nil || latest.Decision == nil {
		t.Fatalf("read latest email decision: %v", err)
	}
	resolve := func(p value.Principal, decisionRef, externalRef, digest string) error {
		_, err := service.ResolveEmailReconciliation(ctx, p, latest.Receipt.Ref, decisionRef, externalRef, digest)
		return err
	}
	if err := resolve(worker, latest.Decision.Ref, latest.Receipt.ExternalReceiptRef, latest.Receipt.ExternalReceiptDigest); err != nil {
		t.Fatalf("resolve exact email decision: %v", err)
	}
	if err := resolve(worker, "", latest.Receipt.ExternalReceiptRef, latest.Receipt.ExternalReceiptDigest); err != nil {
		t.Fatalf("resolve owner-selected email decision: %v", err)
	}
	for _, kind := range []string{"worker", "decision", "external-ref", "digest"} {
		p, decision, externalRef, digest := worker, latest.Decision.Ref, latest.Receipt.ExternalReceiptRef, latest.Receipt.ExternalReceiptDigest
		switch kind {
		case "worker":
			p.CallerWorkload = "integration-gateway"
		case "decision":
			decision = result.EmailDecision.Ref
		case "external-ref":
			externalRef = strings.Repeat("a", 32)
		case "digest":
			digest = strings.Repeat("a", 64)
		}
		if err := resolve(p, decision, externalRef, digest); err == nil {
			t.Fatalf("email decision accepted wrong %s", kind)
		}
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "email-manager-revoke", ExpectedVersion: &binding.AccessBinding.Version},
		Payload:  command.AccessBindingInput{BindingRef: binding.AccessBinding.Ref}}); err != nil {
		t.Fatalf("revoke email manager: %v", err)
	}
	if _, err := service.Execute(ctx, candidateCommand); err == nil {
		t.Fatal("revoked manager replay returned a reconciliation grant")
	}
	if err := resolve(worker, latest.Decision.Ref, latest.Receipt.ExternalReceiptRef, latest.Receipt.ExternalReceiptDigest); err == nil {
		t.Fatal("revoked manager decision remained executable")
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.email_effect_receipts SET external_receipt_digest=repeat('d',64),version=2,outcome='EFFECT_CONFIRMED' WHERE ref='emrc_email_fixture'`); err == nil {
		t.Fatal("email immutable source digest changed")
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.email_effect_receipts SET version=version+1,outcome='EFFECT_CONFIRMED',updated_at=clock_timestamp() WHERE ref='emrc_email_fixture'`); err != nil {
		t.Fatalf("confirm email receipt observation: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.email_effect_receipts SET version=version+1,outcome='NO_EFFECT_CONFIRMED',updated_at=clock_timestamp() WHERE ref='emrc_email_fixture'`); err == nil {
		t.Fatal("terminal email outcome was replaced")
	}
	var observations int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.email_effect_observations o JOIN control_plane.email_effect_receipts e ON e.id=o.receipt_id WHERE e.ref='emrc_email_fixture' AND ((o.version=1 AND o.outcome='UNKNOWN_OUTCOME') OR (o.version=2 AND o.outcome='EFFECT_CONFIRMED'))`).Scan(&observations); err != nil || observations != 2 {
		t.Fatalf("email source observation history: count=%d err=%v", observations, err)
	}
}
