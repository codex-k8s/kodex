package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// personalEmailInvalidPinQuery attempts one duplicate using controlled substituted owner fields.
// A valid duplicate must fail23505; each wrong tuple must fail23514 before uniqueness or FKs.
// personalEmailInvalidPinQuery пробует один duplicate с контролируемой подменой owner fields.
// Допустимый duplicate обязан дать23505, неверный tuple —23514 до uniqueness либо FK.
//
//go:embed testdata/sql/personal_email_invalid_pin.sql
var personalEmailInvalidPinQuery string

// personalEmailReadRuntimeFixture grants independent project/connection metadata permissions via
// actual admin commands, never via personal creation. The wrong root actor is rejected before
// launch; the owner traverses real launch/runtime/invocation claim and READ authorization without
// a generic token. Cancellation is authoritative local cleanup, not a remote mailbox effect.
// personalEmailReadRuntimeFixture выдаёт независимые project/connection metadata permissions
// настоящими admin commands, а не личным create. Чужой root actor отклоняется до launch; владелец
// проходит реальные launch/runtime/invocation claim и READ authorization без generic token.
// Cancel — авторитетная локальная очистка, а не remote mailbox effect.
// The returned observer runs after the caller revokes ownership when pendingRevoke is true.
// Возвращённый observer выполняется после отзыва владельца, если pendingRevoke=true.
func personalEmailReadRuntimeFixture(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, admin, owner, other value.Principal, projects []string, connection entity.IntegrationConnection, otherConnection string, pendingRevoke bool) func() {
	t.Helper()
	t.Log("personal email runtime: resolve stored actors")
	resolvedOwner, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal("resolve personal mailbox principal", err)
	}
	resolvedOther, err := r.ResolvePrincipal(ctx, other)
	if err != nil {
		t.Fatal("resolve alternate mailbox principal", err)
	}
	a, err := r.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal("resolve personal mailbox root actor", err)
	}
	b, err := r.resolveScope(ctx, resolvedOther)
	if err != nil {
		t.Fatal("resolve alternate mailbox root actor", err)
	}
	for index, actor := range []scope{a, b} {
		previewAuthorityGrant(t, ctx, service, admin, "email-runtime-project-"+actor.actorRef, actor.actorRef,
			[]string{"project.view", "agent.view", "agent.launch", "run.view"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: projects[index]})
	}
	t.Log("personal email runtime: independent project launch rights granted")
	previewAuthorityGrant(t, ctx, service, admin, "email-other-metadata", b.actorRef, []string{"integration.view", "integration.manage"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connection.Ref})
	t.Log("personal email runtime: independent connection metadata rights granted")
	agents := []entity.Agent{
		createLifecycleAgent(t, ctx, service, admin, projects[0], "email-runtime-agent-a", "Почтовый агент первого проекта"),
		createLifecycleAgent(t, ctx, service, admin, projects[1], "email-runtime-agent-b", "Почтовый агент второго проекта"),
		createLifecycleAgent(t, ctx, service, admin, projects[0], "email-runtime-agent-child", "Делегированный почтовый агент"),
	}
	enabledDelegate, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: admin,
		Mutation: value.Mutation{IdempotencyKey: "email-parent-delegate-capability", ExpectedVersion: &agents[0].Version}, Payload: command.AgentBindingInput{AgentRef: agents[0].Ref, BindingRef: "platform.run.delegate", Enabled: true}})
	if err != nil || enabledDelegate.Agent == nil {
		t.Fatal("configure explicit parent delegation capability", err)
	}
	agents[0] = *enabledDelegate.Agent
	t.Log("personal email runtime: project agents created")
	for _, agent := range agents {
		result, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: admin, Mutation: value.Mutation{IdempotencyKey: "email-read-grant-" + agent.Ref, ExpectedVersion: &connection.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "email.message.list", ApprovalPolicy: "NONE", Enabled: true}})
		if err != nil || result.Connection == nil {
			t.Fatal("grant explicit email READ to project agent", err)
		}
		connection = *result.Connection
	}
	for _, grant := range []command.IntegrationGrantInput{
		{ConnectionRef: connection.Ref, AgentRef: agents[0].Ref, CapabilityKey: "email.draft.create", ApprovalPolicy: "HUMAN_SCOPED", ApprovalScopePaths: []string{"/folder", "/to", "/cc", "/bcc"}, Enabled: true},
		{ConnectionRef: connection.Ref, AgentRef: agents[0].Ref, CapabilityKey: "email.message.send", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true},
	} {
		result, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: admin,
			Mutation: value.Mutation{IdempotencyKey: "email-native-grant-" + grant.CapabilityKey, ExpectedVersion: &connection.Version}, Payload: grant})
		if err != nil || result.Connection == nil {
			t.Fatal("grant selected personal email approval profile", err)
		}
		connection = *result.Connection
	}
	other.Permission = "platform.email-mailboxes.ownership.read"
	if _, err := service.GetEmailMailboxOwnership(ctx, other, connection.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("independent metadata rights transferred mailbox ownership", err)
	}
	other.Permission = "platform.command.runs.launch"
	_, err = service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: other, Mutation: value.Mutation{IdempotencyKey: "email-other-root-denied"}, Payload: command.LaunchRunInput{ProjectRef: projects[1], Target: entity.RunTarget{Type: "AGENT", Ref: agents[1].Ref}, Task: "Read with another user's mailbox"}})
	var preflight *platformservice.IntegrationCredentialPreflightError
	if !errors.As(err, &preflight) || len(preflight.Issues) != 1 || preflight.Issues[0].ConnectionRef != connection.Ref || preflight.Issues[0].Problem != entity.IntegrationCredentialMissing {
		t.Fatal("wrong mailbox root actor lost exact preflight denial", err)
	}
	owner.Permission = "platform.command.runs.launch"
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "email-personal-read-root"}, Payload: command.LaunchRunInput{ProjectRef: projects[0], Target: entity.RunTarget{Type: "AGENT", Ref: agents[0].Ref}, Task: "Read own synthetic mailbox"}})
	if err != nil || launched.Run == nil {
		t.Fatal("launch personal mailbox root", err)
	}
	_, runtime, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, r)
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: runtime, Mutation: value.Mutation{IdempotencyKey: "email-personal-runtime-claim"}, Payload: command.LeaseInput{WorkloadInstance: "personal-email-runtime", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 || stringMap(claimed.RuntimeItems[0], "runRef") != launched.Run.Ref {
		t.Fatal("claim personal mailbox runtime", err)
	}
	childLease := personalEmailDelegatedReadFixture(t, ctx, r, service, runtime, gateway, claimed.RuntimeItems[0], launched.Run.Ref, connection.Ref, agents[2].Ref, agents[1].Ref, a.actorRef)
	if pendingRevoke {
		personalEmailRevokeBatchFixture(t, ctx, service, runtime, claimed.RuntimeItems[0], launched.Run.Ref, connection.Ref)
	}
	invocation, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{"run_ref": launched.Run.Ref, "node_ref": stringMap(claimed.RuntimeItems[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "email.message.list", "idempotency_key": "email-personal-read-call"}, map[string]any{})
	if err != nil || stringMap(invocation, "state") != "READY" || stringMap(invocation, "gateRef") != "" {
		t.Fatal("personal NONE READ required a gate or lost owner admission", err)
	}
	for _, substitution := range []string{"valid-duplicate", "organization", "connection", "root", "initiator", "owner", "generation", "mailbox", "revision", "digest"} {
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, personalEmailInvalidPinQuery, pgx.StrictNamedArgs{"bad_case": substitution, "other_subject": b.actorID, "other_connection": otherConnection, "invocation_ref": stringMap(invocation, "invocationRef")})
		_ = tx.Rollback(ctx)
		want := "23514"
		if substitution == "valid-duplicate" {
			want = "23505"
		}
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code != want {
			t.Fatal("immutable email pin guard did not reject exact substituted tuple", substitution, err)
		}
	}
	work, err := service.ClaimIntegrationInvocations(ctx, gateway, "personal-email-read-gateway", 1)
	if err != nil || len(work) != 1 || stringMap(work[0], "invocationRef") != stringMap(invocation, "invocationRef") || work[0]["credential"] != nil {
		t.Fatal("claim owner-pinned email READ without generic token", err)
	}
	testEmailGatewayClaim(t, work[0], "invocation")
	if required, _ := work[0]["credentialAdmissionRequired"].(bool); required {
		t.Fatal("email READ entered generic token Begin")
	}
	configuration, err := r.EmailConfiguration(ctx)
	if err != nil || len(configuration.Mailboxes) != 1 {
		t.Fatal("read accepted personal mailbox configuration", err)
	}
	mailbox := configuration.Mailboxes[0]
	semantic, err := api.CommandForIntegration("email.message.list", mailbox.Id, mailbox.Sender, stringMap(work[0], "effectKey"), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	binding := entity.EmailExecutionBinding{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: work[0]["generation"].(int64), ExpiresAt: work[0]["expiresAt"].(time.Time)}
	input := query.EmailAuthorization{Binding: binding, MailboxRef: mailbox.Id, ConfigurationRevision: mailbox.Revision, Operation: "list", Sender: mailbox.Sender, Folder: mailbox.Folder, SemanticInputDigest: api.Digest(semantic), EffectKey: semantic.EffectKey}
	email := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "email-bridge", Operation: "platform.email.authorization.resolve"}, "email-bridge")
	decision, err := service.ResolveEmailAuthorization(ctx, email, input)
	if err != nil || !decision.Allowed || decision.GateApproved || decision.ActorRef != a.actorRef {
		t.Fatal("email READ substituted approver or worker for root initiator", err)
	}
	positive, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{"run_ref": launched.Run.Ref, "node_ref": stringMap(claimed.RuntimeItems[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "email.message.list", "idempotency_key": "email-positive-read-success"}, map[string]any{})
	if err != nil || stringMap(positive, "state") != "READY" {
		t.Fatal("create independent current READ completion", err)
	}
	positiveClaims, err := service.ClaimIntegrationInvocations(ctx, gateway, "email-positive-read-worker", 1)
	if err != nil || len(positiveClaims) != 1 || stringMap(positiveClaims[0], "invocationRef") != stringMap(positive, "invocationRef") {
		t.Fatal("claim exact positive READ completion", err)
	}
	positiveWork := positiveClaims[0]
	testEmailGatewayClaim(t, positiveWork, "invocation")
	completed, err := personalEmailCompleteReadFixture(ctx, service, gateway, "email-positive-read-complete", positiveWork, "successful synthetic read")
	if err != nil || completed.Duplicate {
		t.Fatal("current mailbox READ could not publish its first exact result", err)
	}
	mutation, pending, confirmAdmittedFact := personalEmailMutationRuntimeFixture(t, ctx, service, admin, owner, runtime, gateway, email, a.actorRef,
		launched.Run.Ref, stringMap(claimed.RuntimeItems[0], "nodeRef"), connection.Ref, mailbox)
	queued, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{"run_ref": launched.Run.Ref, "node_ref": stringMap(claimed.RuntimeItems[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "email.message.list", "idempotency_key": "email-read-pending-revoke"}, map[string]any{})
	if err != nil || stringMap(queued, "state") != "READY" {
		t.Fatal("queue fresh mailbox READ for the selected lifecycle transition", err)
	}
	late, err := service.ClaimIntegrationInvocations(ctx, gateway, "email-late-read-before-revoke", 1)
	if err != nil || len(late) != 1 || stringMap(late[0], "invocationRef") != stringMap(queued, "invocationRef") {
		t.Fatal("claim fresh READ before the selected lifecycle transition", err)
	}
	lateRead := late[0]
	freshSemantic, err := api.CommandForIntegration("email.message.list", mailbox.Id, mailbox.Sender, stringMap(lateRead, "effectKey"), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	observerAuthorization := query.EmailAuthorization{Binding: entity.EmailExecutionBinding{InvocationRef: stringMap(lateRead, "invocationRef"), LeaseRef: stringMap(lateRead, "leaseRef"), Fence: stringMap(lateRead, "fence"), Generation: lateRead["generation"].(int64), ExpiresAt: lateRead["expiresAt"].(time.Time)},
		MailboxRef: mailbox.Id, ConfigurationRevision: mailbox.Revision, Operation: "list", Sender: mailbox.Sender, Folder: mailbox.Folder, SemanticInputDigest: api.Digest(freshSemantic), EffectKey: freshSemantic.EffectKey}
	freshDecision, err := service.ResolveEmailAuthorization(ctx, email, observerAuthorization)
	if err != nil || !freshDecision.Allowed || freshDecision.ActorRef != a.actorRef {
		t.Fatal("current mailbox did not authorize the fresh observation binding", err)
	}
	completionVariants := map[string]map[string]any{}
	if pendingRevoke {
		for _, variant := range []string{"failed-partial", "unknown-partial", "empty-failure"} {
			key := "email-read-result-" + variant
			proposed, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{"run_ref": launched.Run.Ref, "node_ref": stringMap(claimed.RuntimeItems[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": "email.message.list", "idempotency_key": key}, map[string]any{})
			if err != nil || stringMap(proposed, "state") != "READY" {
				t.Fatal("prepare independent result variant READ", variant, err)
			}
			items, err := service.ClaimIntegrationInvocations(ctx, gateway, key+"-worker", 1)
			if err != nil || len(items) != 1 || stringMap(items[0], "invocationRef") != stringMap(proposed, "invocationRef") {
				t.Fatal("claim exact result variant READ", variant, err)
			}
			completionVariants[variant] = items[0]
		}
	}
	return func() {
		if time.Until(observerAuthorization.Binding.ExpiresAt) < 3*time.Second {
			t.Fatal("lifecycle denial requires a fresh valid observation binding")
		}
		if pendingRevoke {
			confirmAdmittedFact()
			current, err := service.GetIntegrationInvocation(ctx, gateway, stringMap(lateRead, "invocationRef"))
			if err != nil || stringMap(current, "state") != "RUNNING" {
				t.Fatal("mailbox authorization counterfactual requires a running source before metadata cleanup", err)
			}
			if _, err := service.ResolveEmailAuthorization(ctx, email, observerAuthorization); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("revoked mailbox retained current READ authorization", err)
			}
			replayed, err := personalEmailCompleteReadFixture(ctx, service, gateway, "email-historical-read-replay-after-revoke", positiveWork, "successful synthetic read")
			if err != nil || !replayed.Duplicate {
				t.Fatal("revocation changed exact historical successful READ fact replay", err)
			}
			if _, err := personalEmailCompleteReadFixture(ctx, service, gateway, "email-historical-read-tamper-after-revoke", positiveWork, "tampered historical result"); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("historical READ replay accepted a changed result", err)
			}
			completeFailure := func(variant, summary string) error {
				item := completionVariants[variant]
				stored, err := service.GetIntegrationInvocation(ctx, gateway, stringMap(item, "invocationRef"))
				if err != nil || stringMap(stored, "state") != "RUNNING" || time.Until(item["expiresAt"].(time.Time)) < 3*time.Second {
					t.Fatal("partial result counterfactual requires an independent live READ lease", variant, err)
				}
				unknown := variant == "unknown-partial"
				code := "INTEGRATION_REQUEST_REJECTED"
				if unknown {
					code = "INTEGRATION_OUTCOME_UNKNOWN"
				}
				_, err = service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway,
					Mutation: value.Mutation{IdempotencyKey: "email-revoked-read-" + variant}, Payload: command.IntegrationInvocationInput{
						InvocationRef: stringMap(item, "invocationRef"), LeaseRef: stringMap(item, "leaseRef"), Fence: stringMap(item, "fence"), Generation: item["generation"].(int64),
						UnknownOutcome: unknown, ResultSummary: summary, SafeErrorCode: code}})
				return err
			}
			for _, variant := range []string{"failed-partial", "unknown-partial"} {
				if err := completeFailure(variant, "partial mailbox data after revoke"); !errors.Is(err, errs.ErrForbidden) {
					t.Errorf("non-success READ %s published a partial result after revoke: %v", variant, err)
				}
			}
			if t.Failed() {
				t.FailNow()
			}
			if err := completeFailure("empty-failure", ""); err != nil {
				t.Fatal("data-free failure metadata could not close its exact old READ lease", err)
			}
			empty, err := service.GetIntegrationInvocation(ctx, gateway, stringMap(completionVariants["empty-failure"], "invocationRef"))
			if err != nil || stringMap(empty, "state") != "FAILED" || stringMap(empty, "resultSummary") != "" {
				t.Fatal("empty failure metadata published mailbox data", err)
			}
			stored, err := service.GetIntegrationInvocation(ctx, gateway, stringMap(lateRead, "invocationRef"))
			if err != nil || stringMap(stored, "state") != "RUNNING" || time.Until(lateRead["expiresAt"].(time.Time)) < 3*time.Second {
				t.Fatal("counterfactual requires a live READ lease beyond the bounded cleanup batch", err)
			}
			if _, err := personalEmailCompleteReadFixture(ctx, service, gateway, "email-revoked-read-before-next-batch", lateRead, "stale read"); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("fresh completion bypassed revoked mailbox before next cleanup batch", err)
			}
			remaining, err := service.ClaimIntegrationInvocations(ctx, gateway, "email-drain-next-revoked-batch", 1)
			if err != nil || len(remaining) != 0 {
				t.Fatal("revoked mailbox reentered gateway while draining next metadata batch", err)
			}
			for _, call := range pending {
				stored, err := service.GetIntegrationInvocation(ctx, gateway, stringMap(call, "invocationRef"))
				gate, gateErr := service.GetOwnerGate(ctx, admin, stringMap(call, "gateRef"))
				if err != nil || stringMap(stored, "state") != "CANCELLED" || gateErr != nil || gate.State != "CANCELLED" {
					t.Fatal("owner revoke did not atomically close pending mailbox call and Gate", err, gateErr)
				}
			}
			for _, ref := range []string{stringMap(queued, "invocationRef"), binding.InvocationRef} {
				stored, err := service.GetIntegrationInvocation(ctx, gateway, ref)
				if err != nil || stringMap(stored, "state") != "FAILED" {
					t.Fatal("owner revoke retained queued or running READ lease", err)
				}
			}
			if _, err := personalEmailCompleteReadFixture(ctx, service, gateway, "email-revoked-read-stale-complete", work[0], "stale read"); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("revoked mailbox accepted stale READ completion")
			}
		}
		current, err := service.GetRun(ctx, admin, launched.Run.Ref)
		if err != nil {
			t.Fatal(err)
		}
		cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: admin, Mutation: value.Mutation{IdempotencyKey: "email-personal-read-cancel", ExpectedVersion: &current.Version}, Payload: command.RunCommandInput{RunRef: current.Ref}})
		if err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
			t.Fatal("close synthetic email root", err)
		}
		if _, err := service.ResolveEmailAuthorization(ctx, email, observerAuthorization); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("cancelled root retained mailbox READ authorization", err)
		}
		child, err := service.GetRun(ctx, admin, stringMap(childLease, "runRef"))
		if err != nil || child.State != "CANCELLED" {
			t.Fatal("parent cancellation retained a delegated mailbox child", err)
		}
		stored, err := service.GetIntegrationInvocation(ctx, gateway, mutation)
		if err != nil || stringMap(stored, "state") != "UNKNOWN_OUTCOME" {
			t.Fatal("cancel converted a begun mailbox mutation into known no-effect", err)
		}
	}
}

// personalEmailRevokeBatchFixture creates one hundred distinct real waiting Gates before the
// target READ; duplicate aliasing is rejected. Exact parent lease renewals retain the ordinary
// thirty-second runtime budget. No mailbox effect or approval is performed by this fixture.
// personalEmailRevokeBatchFixture создаёт сто различных настоящих waiting Gates перед целевым
// READ; alias-дубликаты отклоняются. Точные renew parent lease сохраняют штатный thirty-second
// runtime budget. Оснастка не выполняет mailbox effect либо approval.
func personalEmailRevokeBatchFixture(t *testing.T, ctx context.Context, service *platformservice.Service, runtime value.Principal, lease map[string]any, runRef, connectionRef string) {
	t.Helper()
	seen := map[string]bool{}
	for index := range 100 {
		key := fmt.Sprintf("email-revoke-batch-%03d", index)
		if index%10 == 0 {
			if _, err := service.Execute(ctx, command.Command{Kind: command.RenewExecution, Principal: runtime,
				Mutation: value.Mutation{IdempotencyKey: key + "-renew"}, Payload: command.LeaseInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation")}}); err != nil {
				t.Fatal("renew exact parent runtime while preparing bounded cleanup boundary", err)
			}
		}
		result, err := service.ResolveIntegrationInvocation(ctx, runtime, map[string]string{"run_ref": runRef, "node_ref": stringMap(lease, "nodeRef"), "connection_ref": connectionRef, "capability_key": "email.message.send", "idempotency_key": key},
			map[string]any{"to": "recipient@example.test", "subject": key, "body_text": "No effect is approved"})
		ref := stringMap(result, "invocationRef")
		if err != nil || stringMap(result, "state") != "WAITING_APPROVAL" || stringMap(result, "gateRef") == "" || ref == "" || seen[ref] {
			t.Fatal("prepare distinct unapproved mailbox Gate before cleanup target", err)
		}
		seen[ref] = true
	}
}

// personalEmailCompleteReadFixture supplies a complete valid success envelope using exact owner
// claim coordinates and the digest of the proposed result. Denial tests therefore cannot pass
// merely because a required field or digest was missing; no remote mailbox read is performed.
// personalEmailCompleteReadFixture задаёт полный допустимый success envelope из точных owner
// claim coordinates и дайджеста результата. Denial tests не проходят лишь из-за отсутствия
// обязательного поля либо дайджеста; remote mailbox read не выполняется.
func personalEmailCompleteReadFixture(ctx context.Context, service *platformservice.Service, gateway value.Principal, key string, claim map[string]any, summary string) (command.Result, error) {
	digest := sha256.Sum256([]byte(summary))
	return service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway,
		Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.IntegrationInvocationInput{
			InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64),
			Success: true, ResultSummary: summary, EffectKey: stringMap(claim, "effectKey"), InputDigest: stringMap(claim, "inputDigest"), ProviderEffectRef: "synthetic-read-fact", ResponseDigest: hex.EncodeToString(digest[:])}})
}
