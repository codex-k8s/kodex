package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/email_report_shorten_lease.sql
var emailReportShortenLease string

//go:embed testdata/sql/email_report_terminal_source.sql
var emailReportTerminalSource string

func testEmailLateReport(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service,
	owner, runtime, gateway, email, reporter value.Principal, execution map[string]any, connectionRef string, bounded map[string]any, template query.EmailAuthorization) {
	t.Helper()
	for _, key := range []string{"initial", "terminal"} {
		_, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{
			"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"), "connection_ref": connectionRef,
			"capability_key": "email.message.send", "idempotency_key": "email-late-" + key}, bounded)
		if err != nil {
			t.Fatal(err)
		}
	}
	claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "email-late-report", 32)
	if err != nil || len(claims) != 2 {
		t.Fatalf("late report source claims: %d %v", len(claims), err)
	}
	var reports []command.Command
	var expires time.Time
	for index, claim := range claims {
		binding := entity.EmailExecutionBinding{InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64)}
		if err := repository.pool.QueryRow(ctx, emailReportShortenLease, binding.InvocationRef).Scan(&binding.ExpiresAt); err != nil {
			t.Fatal(err)
		}
		expires = binding.ExpiresAt
		raw, _ := json.Marshal(bounded)
		semantic, err := api.CommandForIntegration("email.message.send", template.MailboxRef, template.Sender, stringMap(claim, "effectKey"), raw)
		if err != nil {
			t.Fatal(err)
		}
		input := template
		input.Binding, input.EffectKey, input.SemanticInputDigest = binding, semantic.EffectKey, api.Digest(semantic)
		if _, err := service.ResolveEmailAuthorization(ctx, email, input); err != nil {
			t.Fatalf("issue late source authorization: %v", err)
		}
		key := []string{"initial", "terminal"}[index]
		report := command.Command{Kind: command.ReportEmailEffect, Principal: reporter, Mutation: value.Mutation{IdempotencyKey: "email-late-" + key + "-unknown"},
			Payload: command.EmailEffectReportInput{Binding: binding, ExternalReceiptRef: strings.Repeat([]string{"1", "2"}[index], 32), ExternalReceiptDigest: strings.Repeat("c", 64), SemanticInputDigest: input.SemanticInputDigest, Outcome: "UNKNOWN_OUTCOME"}}
		if index == 1 {
			if _, err := service.Execute(ctx, report); err != nil {
				t.Fatal(err)
			}
			payload := report.Payload.(command.EmailEffectReportInput)
			payload.Outcome = "EFFECT_CONFIRMED"
			report.Payload = payload
			report.Mutation.IdempotencyKey = "email-late-terminal-confirmed"
		}
		reports = append(reports, report)
	}
	timer := time.NewTimer(time.Until(expires) + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	for index, report := range reports {
		payload := report.Payload.(command.EmailEffectReportInput)
		wrongFence := report
		wrongPayload := payload
		wrongPayload.Binding.Fence += "-other"
		wrongFence.Payload = wrongPayload
		if _, err := service.Execute(ctx, wrongFence); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("late foreign source binding accepted: %v", err)
		}
		if index == 0 {
			invalid := report
			wrong := payload
			wrong.Outcome = "EFFECT_CONFIRMED"
			invalid.Payload = wrong
			if _, err := service.Execute(ctx, invalid); !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("late initial terminal accepted: %v", err)
			}
		}
		result, err := service.Execute(ctx, report)
		if err != nil || result.EmailReceipt == nil {
			t.Fatalf("late report %d: %v", index, err)
		}
		replay, err := service.Execute(ctx, report)
		if err != nil || replay.EmailReceipt == nil || replay.EmailReceipt.Ref != result.EmailReceipt.Ref {
			t.Fatalf("late report exact replay: %v", err)
		}
		if index == 1 && (result.EmailReceipt.Version != 2 || result.EmailReceipt.Outcome != "EFFECT_CONFIRMED") {
			t.Fatal("late terminal observation lost")
		}
		if index == 0 {
			fresh := owner
			fresh.CredentialAuthenticatedAt = time.Now().Add(-time.Minute)
			fresh.CredentialACR = "urn:test:interactive"
			fresh.CredentialAMR = []string{"pwd"}
			decision, err := service.Execute(ctx, command.Command{Kind: command.ReconcileEmailEffect, Principal: fresh,
				Mutation: value.Mutation{IdempotencyKey: "email-late-owner-decision", ExpectedVersion: &result.EmailReceipt.Version},
				Payload:  command.EmailReconciliationInput{ReceiptRef: result.EmailReceipt.Ref, ExpectedReceiptDigest: payload.ExternalReceiptDigest, Outcome: "NO_EFFECT_CONFIRMED", Note: "No provider effect before initial receipt"}})
			if err != nil || decision.EmailDecision == nil {
				t.Fatalf("late initial owner resolution: %v", err)
			}
			resolved, err := service.ResolveEmailReconciliation(ctx, email, result.EmailReceipt.Ref, "", payload.ExternalReceiptRef, payload.ExternalReceiptDigest)
			if err != nil || resolved.Decision == nil || resolved.Decision.GrantRef == "" {
				t.Fatalf("late source unlock decision: %v", err)
			}
			for _, state := range []string{"CANCELLED", "FAILED"} {
				if _, err := repository.pool.Exec(ctx, emailReportTerminalSource, payload.Binding.InvocationRef, state); err != nil {
					t.Fatal(err)
				}
				_, err := service.Execute(ctx, command.Command{Kind: command.ReconcileEmailEffect, Principal: fresh,
					Mutation: value.Mutation{IdempotencyKey: "email-late-owner-" + state, ExpectedVersion: &result.EmailReceipt.Version},
					Payload:  command.EmailReconciliationInput{ReceiptRef: result.EmailReceipt.Ref, ExpectedReceiptDigest: payload.ExternalReceiptDigest, Outcome: "NO_EFFECT_CONFIRMED", Note: "Closed source is not retried"}})
				if err != nil {
					t.Fatalf("closed source reconciliation %s: %v", state, err)
				}
				if _, err := service.ResolveEmailReconciliation(ctx, email, result.EmailReceipt.Ref, "", payload.ExternalReceiptRef, payload.ExternalReceiptDigest); err != nil {
					t.Fatalf("closed source decision read %s: %v", state, err)
				}
			}
			conflicting := report
			payload.Outcome = "EFFECT_CONFIRMED"
			conflicting.Payload = payload
			conflicting.Mutation.IdempotencyKey = "email-late-contradict-owner"
			if _, err := service.Execute(ctx, conflicting); !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("late observation contradicted owner: %v", err)
			}
		}
	}
	if claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "email-late-no-resend", 32); err != nil || len(claims) != 0 {
		t.Fatalf("late recovery retried provider source: %d %v", len(claims), err)
	}
}
