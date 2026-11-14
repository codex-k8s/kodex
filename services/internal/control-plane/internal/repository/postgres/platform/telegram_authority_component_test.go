package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// telegramManagerFixturePrincipal resolves a non-owner transport subject under an exact project and operation, preserving the server-resolved tenant and project rather than copying payload identifiers.
// telegramManagerFixturePrincipal разрешает transport subject без owner claim для точных проекта и операции, сохраняя разрешённые сервером tenant и project вместо копирования payload identifiers.
func telegramManagerFixturePrincipal(ctx context.Context, r *Repository, projectRef, operation string) (value.Principal, error) {
	authority, err := r.ResolveProofAuthority(ctx, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000008461", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: operation, ProjectRef: projectRef})
	if err != nil {
		return value.Principal{}, err
	}
	return value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID, ProjectRef: authority.ProjectID, Permission: operation, CallerWorkload: "control-api-gateway", CorrelationRef: "telegram-manager-authority-component", CredentialRevision: 1}, nil
}

// testTelegramApproverAuthorityComponent checks an explicitly scoped non-owner approver loses admission immediately after project or connection authority is revoked, including cached Configure receipt replay and queued claims.
// testTelegramApproverAuthorityComponent проверяет немедленную потерю admission явно scoped approver без owner claim после отзыва project либо connection authority, включая cached Configure receipt replay и queued claims.
func testTelegramApproverAuthorityComponent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, r *Repository, service *serviceplatform.Service) {
	t.Helper()
	owner := telegramFixturePrincipal(t, ctx, r, "platform.command.projects.create", "", false)
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-authority-project"}, Payload: command.ProjectInput{Name: "Telegram scoped approver", Purpose: "Synthetic current authority recheck", Language: "ru"}})
	if err != nil || project.Project == nil {
		t.Fatal("create Telegram approver project", err)
	}
	projectRef := project.Project.Ref
	agent := createLifecycleAgent(t, ctx, service, owner, projectRef, "telegram-authority-agent", "Telegram authority agent")
	connection := telegramCreateFixtureConnection(t, ctx, r, service, map[string]any{"base_url": integrationpackage.TelegramAPIOrigin, "bot_id": float64(123456), "chat_id": "-1001234", "topic": "77", "quiet_enabled": false}, "telegram-authority-fixture")
	reader := contextProjectReader(t, ctx, r, service, owner, projectRef, "TELEGRAM_AUTHORITY")
	actor, err := r.ResolvePrincipal(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	proposal := port.TelegramSubscriptionConfigure{ProjectRef: projectRef, ConnectionRef: connection.Ref, Enabled: true, EventTypes: []string{"FINISHED"}, DurationSeconds: 3600, MaxEffects: 4}
	p, proofErr := telegramManagerFixturePrincipal(ctx, r, projectRef, "platform.command.telegram-subscription.configure")
	if proofErr == nil {
		if _, err := service.ConfigureTelegramSubscription(ctx, p, value.Mutation{IdempotencyKey: "telegram-reader-cannot-consent"}, proposal); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("project viewer issued project notification consent", err)
		}
	} else if !errors.Is(proofErr, errs.ErrForbidden) && !errors.Is(proofErr, errs.ErrNotFound) {
		t.Fatal("viewer proof failed unexpectedly", proofErr)
	}
	projectBinding := receiptAccessBinding(t, ctx, service, owner, actor.ActorID, "telegram-approver-project-manage", []string{"project.manage", "run.view"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: projectRef})
	connectionBinding := receiptAccessBinding(t, ctx, service, owner, actor.ActorID, "telegram-approver-connection-manage", []string{"integration.view", "integration.manage"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connection.Ref})
	p, err = telegramManagerFixturePrincipal(ctx, r, projectRef, "platform.command.telegram-subscription.configure")
	if err != nil {
		t.Fatal("resolve scoped approver", err)
	}
	consent := value.Mutation{IdempotencyKey: "telegram-scoped-approver-enable"}
	subscription, err := service.ConfigureTelegramSubscription(ctx, p, consent, proposal)
	if err != nil || !subscription.Enabled {
		t.Fatal("scoped non-owner could not approve exact project subscription", err)
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_authority_before_begin_fixture")
	first := telegramClaimFixture(t, ctx, r, service)
	owner = telegramFixturePrincipal(t, ctx, r, "platform.access.bindings.revoke", "", false)
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-approver-project-revoke", ExpectedVersion: &projectBinding.Version}, Payload: command.AccessBindingInput{BindingRef: projectBinding.Ref}}); err != nil {
		t.Fatal("revoke exact approver project authority", err)
	}
	worker := telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.begin", "", true)
	if admission, err := service.BeginInteractionDelivery(ctx, worker, first); err != nil || admission.Allowed || admission.AlreadyStarted || admission.SafeErrorCode != "TELEGRAM_SCOPE_REVOKED" {
		t.Fatal("revoked approver retained pre-send authority", err)
	}
	if _, err := service.ConfigureTelegramSubscription(ctx, p, consent, proposal); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("cached owner receipt bypassed current project authority", err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.claim", "", true)
	if claims, err := service.ClaimInteractionDeliveries(ctx, worker, "telegram-owner-fixture", 1); err != nil || len(claims) != 0 {
		t.Fatal("revoked approver retained claim eligibility", err)
	}
	var closed bool
	if err := pool.QueryRow(ctx, telegramSubscriptionClosedFixture, subscription.Ref).Scan(&closed); err != nil || !closed {
		t.Fatal("revoked approver did not durably close consent and leases", err)
	}
	receiptAccessBinding(t, ctx, service, owner, actor.ActorID, "telegram-approver-project-restored", []string{"project.manage", "run.view"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: projectRef})
	viewer := telegramFixturePrincipal(t, ctx, r, "platform.query.telegram-subscription.get", projectRef, false)
	latest, canManage, err := service.GetTelegramSubscription(ctx, viewer, projectRef)
	if err != nil || latest == nil || !canManage {
		t.Fatal("read current scoped subscription", err)
	}
	p, err = telegramManagerFixturePrincipal(ctx, r, projectRef, "platform.command.telegram-subscription.configure")
	if err != nil {
		t.Fatal(err)
	}
	subscription, err = service.ConfigureTelegramSubscription(ctx, p, value.Mutation{IdempotencyKey: "telegram-scoped-approver-renew", ExpectedVersion: &latest.Version}, proposal)
	if err != nil {
		t.Fatal("explicitly approve fresh authority scope", err)
	}
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_authority_before_claim_fixture")
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-approver-connection-revoke", ExpectedVersion: &connectionBinding.Version}, Payload: command.AccessBindingInput{BindingRef: connectionBinding.Ref}}); err != nil {
		t.Fatal("revoke exact approver connection authority", err)
	}
	if claims, err := service.ClaimInteractionDeliveries(ctx, worker, "telegram-owner-fixture", 1); err != nil || len(claims) != 0 {
		t.Fatal("connection authority revoke still issued private claim data", err)
	}
	if err := pool.QueryRow(ctx, telegramSubscriptionClosedFixture, subscription.Ref).Scan(&closed); err != nil || !closed {
		t.Fatal("connection authority revoke retained grant or queue", err)
	}
	viewer = telegramFixturePrincipal(t, ctx, r, "platform.query.interaction-deliveries.list", projectRef, false)
	history, _, err := service.ListInteractionDeliveries(ctx, viewer, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(history) != 2 {
		t.Fatal("authority revoke lost retained source history", err)
	}
	for _, delivery := range history {
		if delivery.State != "CANCELLED" || delivery.Retryable {
			t.Fatal("revoked unsent event remained repeatable")
		}
	}
	if _, err := r.ResolveProofAuthority(ctx, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000008461", ExternalTenantID: "20000000-0000-4000-8000-000000000099", CallerWorkload: "control-api-gateway", Operation: "platform.query.telegram-subscription.get", ProjectRef: projectRef}); !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("foreign tenant transport resolved the retained subscription", err)
	}
}
