package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
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

// personalEmailMutationRuntimeFixture uses explicit test-user Gate decisions through the real
// owner transaction: draft consent covers exact recipients/folder, body edits reuse its bounded
// ledger, hidden cc/bcc and exhaustion require new Gates, and send always uses EACH. It records
// the durable pre-effect UNKNOWN receipt but performs no SMTP/IMAP mutation or live approval.
// personalEmailMutationRuntimeFixture использует явные test-user Gate decisions через настоящую
// owner transaction: согласие черновика охватывает точные recipients/folder, body edits используют
// ограниченный ledger, скрытые cc/bcc и исчерпание требуют новых Gates, send всегда EACH. Тест
// записывает durable pre-effect UNKNOWN receipt, но не выполняет SMTP/IMAP mutation либо live approval.
func personalEmailMutationRuntimeFixture(t *testing.T, ctx context.Context, service *platformservice.Service, approver, rootOwner, runtime, gateway, email value.Principal, rootActor, runRef, nodeRef, connectionRef string, mailbox api.Mailbox) (string, []map[string]any, func()) {
	t.Helper()
	call := func(key, capability string, input map[string]any) map[string]any {
		t.Helper()
		result, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{
			"run_ref": runRef, "node_ref": nodeRef, "connection_ref": connectionRef, "capability_key": capability, "idempotency_key": key}, input)
		if err != nil {
			t.Fatal("resolve exact personal mailbox mutation", key, err)
		}
		return result
	}
	approve := func(key string, invocation map[string]any, selection *command.IntegrationApprovalSelection) {
		t.Helper()
		gate, err := service.GetOwnerGate(ctx, approver, stringMap(invocation, "gateRef"))
		if err != nil || gate.State != "OPEN" {
			t.Fatal("read actual personal email Gate", err)
		}
		result, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: approver,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &gate.Version}, Payload: command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", IntegrationApproval: selection}})
		if err != nil || result.Gate == nil || result.Gate.State != "APPROVED" {
			t.Fatal("apply explicit synthetic email Gate decision", err)
		}
	}
	claim := func(key, ref string) map[string]any {
		t.Helper()
		work, err := service.ClaimIntegrationInvocations(ctx, gateway, key, 32)
		if err != nil {
			t.Fatal("claim personal email mutation", err)
		}
		for _, item := range work {
			if stringMap(item, "invocationRef") == ref {
				testEmailGatewayClaim(t, item, "invocation")
				return item
			}
		}
		t.Fatal("approved personal email mutation was not claimed")
		return nil
	}
	authorize := func(item map[string]any, operation string, input map[string]any, wantGate bool) query.EmailAuthorization {
		t.Helper()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		semantic, err := api.CommandForIntegration(operation, mailbox.Id, mailbox.Sender, stringMap(item, "effectKey"), raw)
		if err != nil {
			t.Fatal("derive personal mutation semantic input", err)
		}
		folder := semantic.Folder
		if folder == "" {
			folder = mailbox.Folder
		}
		request := query.EmailAuthorization{Binding: entity.EmailExecutionBinding{InvocationRef: stringMap(item, "invocationRef"), LeaseRef: stringMap(item, "leaseRef"), Fence: stringMap(item, "fence"), Generation: item["generation"].(int64), ExpiresAt: item["expiresAt"].(time.Time)},
			MailboxRef: mailbox.Id, ConfigurationRevision: mailbox.Revision, Operation: string(semantic.Operation), Sender: mailbox.Sender, Folder: folder, SemanticInputDigest: api.Digest(semantic), EffectKey: semantic.EffectKey}
		decision, err := service.ResolveEmailAuthorization(ctx, email, request)
		if err != nil || !decision.Allowed || decision.GateApproved != wantGate || decision.ActorRef != rootActor {
			t.Fatal("personal mutation lost root credential owner after another user's approval", err)
		}
		return request
	}
	noEffect := func(key string, item map[string]any) {
		t.Helper()
		if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.IntegrationInvocationInput{InvocationRef: stringMap(item, "invocationRef"), LeaseRef: stringMap(item, "leaseRef"), Fence: stringMap(item, "fence"), Generation: item["generation"].(int64), SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"}}); err != nil {
			t.Fatal("finish synthetic pre-Begin no-effect mutation", err)
		}
	}
	draftInput := map[string]any{"folder": "Drafts", "to": "recipient@example.test", "cc": "[]", "bcc": "[]", "subject": "First draft", "body_text": "Initial body"}
	first := call("email-draft-scoped-first", "email.draft.create", draftInput)
	if stringMap(first, "state") != "WAITING_APPROVAL" || stringMap(first, "gateRef") == "" {
		t.Fatal("draft did not require initial scoped consent")
	}
	approve("email-draft-scoped-approve", first, &command.IntegrationApprovalSelection{DurationSeconds: 600, MaxEffects: 2, Lifetime: "ROOT_RUN"})
	firstWork := claim("email-draft-first-worker", stringMap(first, "invocationRef"))
	authorize(firstWork, "email.draft.create", draftInput, true)
	noEffect("email-draft-first-no-effect", firstWork)
	draftInput["subject"], draftInput["body_text"] = "Edited subject", "Edited body"
	var simultaneous [2]map[string]any
	var failures [2]error
	var joined sync.WaitGroup
	for index, key := range []string{"email-draft-scoped-second", "email-draft-scoped-competing"} {
		joined.Go(func() {
			simultaneous[index], failures[index] = service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{
				"run_ref": runRef, "node_ref": nodeRef, "connection_ref": connectionRef, "capability_key": "email.draft.create", "idempotency_key": key}, draftInput)
		})
	}
	joined.Wait()
	var second, racedGate map[string]any
	for index, result := range simultaneous {
		if failures[index] != nil {
			t.Fatal("resolve competing calls against one remaining consent effect", failures[index])
		}
		if stringMap(result, "state") == "READY" && stringMap(result, "gateRef") == "" && second == nil {
			second = result
		} else if stringMap(result, "state") == "WAITING_APPROVAL" && stringMap(result, "gateRef") != "" && racedGate == nil {
			racedGate = result
		} else {
			t.Fatal("competing mailbox calls exceeded or lost the single remaining scoped effect")
		}
	}
	if second == nil || racedGate == nil {
		t.Fatal("one remaining scoped effect did not produce exactly one admitted call")
	}
	secondWork := claim("email-draft-second-worker", stringMap(second, "invocationRef"))
	authorize(secondWork, "email.draft.create", draftInput, false)
	noEffect("email-draft-second-no-effect", secondWork)
	var pending []map[string]any
	for _, field := range []string{"cc", "bcc"} {
		draftInput[field] = `["copy@example.test"]`
		changed := call("email-draft-hidden-"+field, "email.draft.create", draftInput)
		if stringMap(changed, "state") != "WAITING_APPROVAL" || stringMap(changed, "gateRef") == "" {
			t.Fatal("hidden recipient change reused prior consent", field)
		}
		pending = append(pending, changed)
		draftInput[field] = "[]"
	}
	exhausted := call("email-draft-scoped-exhausted", "email.draft.create", draftInput)
	if stringMap(exhausted, "state") != "WAITING_APPROVAL" || stringMap(exhausted, "gateRef") == "" {
		t.Fatal("failed no-effect calls recycled consumed scope budget")
	}
	// An identical pending draft can rejoin the losing concurrent Gate; that exact Gate is
	// explicitly approved below, so it is not part of the later unapproved cancellation set.
	// Одинаковый pending draft может rejoin проигравший concurrent Gate; именно он явно
	// одобряется ниже и не входит в последующий набор неначатых отменяемых Gate.
	if stringMap(exhausted, "invocationRef") != stringMap(racedGate, "invocationRef") {
		pending = append(pending, exhausted)
	}
	rootOwner.Permission = "platform.integration-approval-consents.list"
	consents, _, err := service.ListIntegrationApprovalConsents(ctx, rootOwner, query.Filter{Page: query.Page{Size: 20}})
	if err != nil || len(consents) != 1 || consents[0].State != "EXHAUSTED" || consents[0].ReservedEffects != 2 || consents[0].MaxEffects != 2 {
		t.Fatal("competing calls changed the authoritative exhausted consent counters", err)
	}
	approve("email-draft-initiator-resource-approve", racedGate, &command.IntegrationApprovalSelection{DurationSeconds: 600, MaxEffects: 3, Lifetime: "INITIATOR_RESOURCE"})
	renewedWork := claim("email-draft-initiator-resource-worker", stringMap(racedGate, "invocationRef"))
	authorize(renewedWork, "email.draft.create", draftInput, true)
	noEffect("email-draft-initiator-resource-no-effect", renewedWork)
	awaitingRevoke := call("email-draft-reserved-before-consent-revoke", "email.draft.create", draftInput)
	if stringMap(awaitingRevoke, "state") != "READY" || stringMap(awaitingRevoke, "gateRef") != "" {
		t.Fatal("new initiator-resource consent did not admit its second exact call")
	}
	consents, _, err = service.ListIntegrationApprovalConsents(ctx, rootOwner, query.Filter{Page: query.Page{Size: 20}})
	if err != nil || len(consents) != 2 {
		t.Fatal("read actual mailbox consent before explicit self revoke", err)
	}
	var active entity.IntegrationApprovalConsent
	for _, consent := range consents {
		if consent.State == "ACTIVE" && consent.Lifetime == "INITIATOR_RESOURCE" {
			active = consent
		}
	}
	if active.Ref == "" || active.ReservedEffects != 2 || active.MaxEffects != 3 {
		t.Fatal("explicit revoke scenario did not retain an unused scoped effect")
	}
	rootOwner.Permission = "platform.integration-approval-consents.revoke"
	revoked, err := service.Execute(ctx, command.Command{Kind: command.RevokeIntegrationApprovalConsent, Principal: rootOwner,
		Mutation: value.Mutation{IdempotencyKey: "email-draft-self-consent-revoke", ExpectedVersion: &active.Version}, Payload: command.IntegrationApprovalConsentInput{ConsentRef: active.Ref}})
	if err != nil || revoked.IntegrationApprovalConsent == nil || revoked.IntegrationApprovalConsent.State != "REVOKED" || revoked.IntegrationApprovalConsent.ReservedEffects != 2 {
		t.Fatal("mailbox initiator could not revoke its own remaining consent without rewinding counters", err)
	}
	unclaimed, err := service.ClaimIntegrationInvocations(ctx, gateway, "email-consent-revoke-claim-denied", 32)
	if err != nil || len(unclaimed) != 0 {
		t.Fatal("revoked mailbox consent still authorized a pending gateway claim", err)
	}
	stored, err := service.GetIntegrationInvocation(ctx, gateway, stringMap(awaitingRevoke, "invocationRef"))
	if err != nil || stringMap(stored, "state") != "FAILED" || stringMap(stored, "safeErrorCode") != "INTEGRATION_APPROVAL_REVOKED" {
		t.Fatal("reserved mailbox call remained executable after self consent revoke", err)
	}
	afterRevoke := call("email-draft-call-after-consent-revoke", "email.draft.create", draftInput)
	if stringMap(afterRevoke, "state") != "WAITING_APPROVAL" || stringMap(afterRevoke, "gateRef") == "" {
		t.Fatal("future mailbox draft reused explicitly revoked consent")
	}
	read := call("email-none-read-after-write-consent-revoke", "email.message.list", map[string]any{})
	if stringMap(read, "state") != "READY" || stringMap(read, "gateRef") != "" {
		t.Fatal("NONE READ inherited the revoked write consent requirement")
	}
	readWork := claim("email-none-read-after-revoke-worker", stringMap(read, "invocationRef"))
	authorize(readWork, "email.message.list", map[string]any{}, false)
	noEffect("email-none-read-after-revoke-no-effect", readWork)
	approve("email-draft-short-clock-approve", afterRevoke, &command.IntegrationApprovalSelection{DurationSeconds: 1, MaxEffects: 3, Lifetime: "ROOT_RUN"})
	rootOwner.Permission = "platform.integration-approval-consents.list"
	expiryDeadline := time.Now().Add(5 * time.Second)
	for {
		catalog, _, err := service.ListIntegrationApprovalConsents(ctx, rootOwner, query.Filter{Page: query.Page{Size: 20}})
		if err != nil || len(catalog) != 3 {
			t.Fatal("read real PostgreSQL clock-driven mailbox consent expiry", err)
		}
		expired := false
		for _, consent := range catalog {
			if consent.Lifetime == "ROOT_RUN" && consent.MaxEffects == 3 && consent.ReservedEffects == 1 && consent.State == "EXPIRED" {
				expired = true
			}
		}
		if expired {
			break
		}
		if time.Now().After(expiryDeadline) {
			t.Fatal("authoritative mailbox consent did not expire within bounded real-clock observation")
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("mailbox consent expiry observation was cancelled")
		}
	}
	expiredClaims, err := service.ClaimIntegrationInvocations(ctx, gateway, "email-expired-consent-worker", 32)
	if err != nil || len(expiredClaims) != 0 {
		t.Fatal("expired mailbox consent reached a gateway claim", err)
	}
	expiredCall, err := service.GetIntegrationInvocation(ctx, gateway, stringMap(afterRevoke, "invocationRef"))
	if err != nil || stringMap(expiredCall, "state") != "FAILED" || stringMap(expiredCall, "safeErrorCode") != "INTEGRATION_APPROVAL_EXPIRED" {
		t.Fatal("expired mailbox intent did not close with the exact expiry outcome", err)
	}
	afterExpiry := call("email-draft-call-after-consent-expiry", "email.draft.create", draftInput)
	if stringMap(afterExpiry, "state") != "WAITING_APPROVAL" || stringMap(afterExpiry, "gateRef") == "" {
		t.Fatal("new mailbox intent reused expired consent")
	}
	pending = append(pending, afterExpiry)
	sendInput := map[string]any{"to": "recipient@example.test", "subject": "Synthetic send intent", "body_text": "No network effect is performed"}
	send := call("email-send-each-first", "email.message.send", sendInput)
	if stringMap(send, "state") != "WAITING_APPROVAL" || stringMap(send, "gateRef") == "" {
		t.Fatal("send inherited draft scoped consent or omitted EACH")
	}
	approve("email-send-each-approve", send, nil)
	sendWork := claim("email-send-each-worker", stringMap(send, "invocationRef"))
	request := authorize(sendWork, "email.message.send", sendInput, true)
	bridge := email
	bridge.Permission = "platform.email.effect-receipts.report"
	report := command.Command{Kind: command.ReportEmailEffect, Principal: bridge, Mutation: value.Mutation{IdempotencyKey: "email-native-pre-effect-begin"}, Payload: command.EmailEffectReportInput{Binding: request.Binding, ExternalReceiptRef: strings.Repeat("e", 32), ExternalReceiptDigest: strings.Repeat("a", 64), SemanticInputDigest: request.SemanticInputDigest, Outcome: "UNKNOWN_OUTCOME"}}
	begun, err := service.Execute(ctx, report)
	if err != nil || begun.EmailReceipt == nil || begun.EmailReceipt.Outcome != "UNKNOWN_OUTCOME" {
		t.Fatal("durable current-owner mailbox Begin failed", err)
	}
	replayed, err := service.Execute(ctx, report)
	if err != nil || replayed.EmailReceipt == nil || replayed.EmailReceipt.Ref != begun.EmailReceipt.Ref || replayed.EmailReceipt.Version != begun.EmailReceipt.Version {
		t.Fatal("pre-effect mailbox receipt replay changed durable identity", err)
	}
	confirmAfterRevoke := func() {
		t.Helper()
		if time.Until(request.Binding.ExpiresAt) < 3*time.Second {
			t.Fatal("late admitted fact requires an original lease that has not expired")
		}
		if _, err := service.ResolveEmailAuthorization(ctx, email, request); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("revoked mailbox issued fresh mutation authorization", err)
		}
		known := report
		payload := report.Payload.(command.EmailEffectReportInput)
		payload.Outcome = "EFFECT_CONFIRMED"
		known.Payload = payload
		known.Mutation.IdempotencyKey = "email-admitted-late-fact-after-revoke"
		foreign := known
		bad := payload
		bad.Binding.Fence += "-foreign"
		foreign.Payload = bad
		foreign.Mutation.IdempotencyKey = "email-admitted-late-foreign-fence"
		if _, err := service.Execute(ctx, foreign); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("late admitted fact accepted another original fence", err)
		}
		confirmed, err := service.Execute(ctx, known)
		if err != nil || confirmed.EmailReceipt == nil || confirmed.EmailReceipt.Ref != begun.EmailReceipt.Ref || confirmed.EmailReceipt.Outcome != "EFFECT_CONFIRMED" || confirmed.EmailReceipt.Version != begun.EmailReceipt.Version+1 {
			t.Fatal("revocation discarded a historical admitted mutation fact", err)
		}
		replayed, err := service.Execute(ctx, known)
		if err != nil || replayed.EmailReceipt == nil || replayed.EmailReceipt.Ref != confirmed.EmailReceipt.Ref || replayed.EmailReceipt.Version != confirmed.EmailReceipt.Version {
			t.Fatal("late historical fact replay changed its immutable source", err)
		}
		if _, err := service.ResolveEmailAuthorization(ctx, email, request); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("historical mutation fact became fresh dispatch authority", err)
		}
	}
	return stringMap(send, "invocationRef"), pending, confirmAfterRevoke
}
