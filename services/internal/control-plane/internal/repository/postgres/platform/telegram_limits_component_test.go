package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// telegramCreateFixtureConnection uses exact owner commands for a service bot and projected descriptor, then exercises the real metadata-test ledger with synthetic provider responses.
// telegramCreateFixtureConnection использует exact owner commands служебного бота и projected descriptor, затем проходит реальный metadata-test ledger с synthetic provider responses.
func telegramCreateFixtureConnection(t *testing.T, ctx context.Context, r *Repository, service *serviceplatform.Service, configuration map[string]any, key string) entity.IntegrationConnection {
	t.Helper()
	p := telegramFixturePrincipal(t, ctx, r, "platform.command.integrations.create", "", false)
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: p, Mutation: value.Mutation{IdempotencyKey: key + "-create"}, Payload: command.ConnectionInput{DefinitionKey: "telegram", Name: key, PublicConfiguration: configuration}})
	if err != nil || created.Connection == nil {
		t.Fatal("create additional Telegram connection", err)
	}
	p = telegramFixturePrincipal(t, ctx, r, "platform.command.integrations.configure-credential", "", false)
	configured, err := service.Execute(ctx, command.Command{Kind: command.ConfigureConnectionCredential, Principal: p, Mutation: value.Mutation{IdempotencyKey: key + "-credential", ExpectedVersion: &created.Connection.Version}, Payload: command.ConnectionInput{
		Ref: created.Connection.Ref, MaterializationRef: key, CredentialRevision: &entity.IntegrationCredentialRevision{SecretRef: "kodex-system/kodex-integration-credentials#" + key, SecretUID: "11111111-1111-4111-8111-111111111111", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("d", 64)}}})
	if err != nil || configured.Connection == nil {
		t.Fatal("configure additional projected service credential", err)
	}
	return telegramVerifyFixtureConnection(t, ctx, r, service, created.Connection.Ref, configured.Connection.Version, key)
}

// testTelegramLimitsComponent proves quiet hours, queue/rate bounds, explicit revoke and cross-project signed-context refusal through native owner transitions without sending any message.
// testTelegramLimitsComponent доказывает тихие часы, queue/rate bounds, явный revoke и cross-project signed-context refusal через native owner transitions без отправки сообщений.
func testTelegramLimitsComponent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, r *Repository, service *serviceplatform.Service, mainProject string) {
	t.Helper()
	owner := telegramFixturePrincipal(t, ctx, r, "platform.command.projects.create", "", false)
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-limits-project"}, Payload: command.ProjectInput{Name: "Telegram limits fixture", Purpose: "Synthetic quiet and rate bounds", Language: "ru"}})
	if err != nil || project.Project == nil {
		t.Fatal("create Telegram limits project", err)
	}
	projectRef := project.Project.Ref
	agent := createLifecycleAgent(t, ctx, service, owner, projectRef, "telegram-limits-agent", "Telegram limits agent")
	now := time.Now().UTC()
	configuration := map[string]any{"base_url": integrationpackage.TelegramAPIOrigin, "bot_id": float64(123456), "chat_id": "-1001234", "topic": "77",
		"quiet_enabled": true, "quiet_start": now.Add(-30 * time.Minute).Format("15:04"), "quiet_end": now.Add(30 * time.Minute).Format("15:04"), "quiet_timezone": "UTC", "queue_limit": float64(1), "rate_per_minute": float64(1)}
	connection := telegramCreateFixtureConnection(t, ctx, r, service, configuration, "telegram-limits-fixture")
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.telegram-subscription.configure", projectRef, false)
	proposal := port.TelegramSubscriptionConfigure{ProjectRef: projectRef, ConnectionRef: connection.Ref, Enabled: true, EventTypes: []string{"FINISHED"}, DurationSeconds: 3600, MaxEffects: 10}
	subscription, err := service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-limits-enable"}, proposal)
	if err != nil {
		t.Fatal("enable exact limits subscription", err)
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_quiet_first_fixture")
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_quiet_overflow_fixture")
	viewer := telegramFixturePrincipal(t, ctx, r, "platform.query.interaction-deliveries.list", projectRef, false)
	queued, _, err := service.ListInteractionDeliveries(ctx, viewer, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(queued) != 1 || queued[0].State != "DUE" || !queued[0].AvailableAt.After(now) {
		t.Fatal("producer did not apply quiet scheduling or queue bound", err)
	}
	worker := telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.claim", "", true)
	if claims, err := service.ClaimInteractionDeliveries(ctx, worker, "telegram-owner-fixture", 1); err != nil || len(claims) != 0 {
		t.Fatal("quiet-scheduled queue was claimed early", err)
	}
	// Model a due item crossing into quiet time: Begin must independently recheck the current window after claim.
	// Моделируется due item при входе в тихий интервал: Begin независимо перепроверяет текущее окно после claim.
	if _, err := pool.Exec(ctx, telegramDeliveryClockFixture, pgx.StrictNamedArgs{"delivery_ref": queued[0].Ref, "expire_lease": false}); err != nil {
		t.Fatal(err)
	}
	begin := telegramClaimFixture(t, ctx, r, service)
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.begin", "", true)
	admission, err := service.BeginInteractionDelivery(ctx, worker, begin)
	if err != nil || admission.Allowed || admission.AlreadyStarted || admission.SafeErrorCode != "TELEGRAM_ADMISSION_DEFERRED" {
		t.Fatal("quiet hours admitted an effect or lost pre-send defer", err)
	}
	var used, rate int
	var version int64
	var enabled bool
	if err := pool.QueryRow(ctx, telegramSubscriptionCountersFixture, subscription.Ref).Scan(&used, &rate, &version, &enabled); err != nil || used != 0 || rate != 0 || version != subscription.Version || !enabled {
		t.Fatal("quiet defer consumed quota or renewed consent", err)
	}
	viewer = telegramFixturePrincipal(t, ctx, r, "platform.query.interaction-deliveries.list", projectRef, false)
	history, _, err := service.ListInteractionDeliveries(ctx, viewer, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(history) != 1 || history[0].State != "DUE" || history[0].Attempt != 0 || !history[0].AvailableAt.After(now) {
		t.Fatal("queue overflow or quiet defer did not preserve bounded unsent work", err)
	}
	foreign := telegramFixturePrincipal(t, ctx, r, "platform.query.interaction-deliveries.list", mainProject, false)
	if _, _, err := service.ListInteractionDeliveries(ctx, foreign, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("payload project replaced the signed project boundary", err)
	}
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.telegram-subscription.configure", projectRef, false)
	disabled, err := service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-quiet-revoke", ExpectedVersion: &subscription.Version}, port.TelegramSubscriptionConfigure{ProjectRef: projectRef})
	if err != nil || disabled.Enabled {
		t.Fatal("revoke quiet subscription", err)
	}
	if _, err := service.BeginInteractionDelivery(ctx, worker, begin); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("revoked quiet claim remained live", err)
	}
	configuration = map[string]any{"base_url": integrationpackage.TelegramAPIOrigin, "bot_id": float64(123456), "chat_id": "-1001234", "topic": "77", "quiet_enabled": false, "queue_limit": float64(1), "rate_per_minute": float64(1)}
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.integrations.update", "", false)
	updated, err := service.Execute(ctx, command.Command{Kind: command.UpdateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-limits-rate-config", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref, Name: connection.Name, PublicConfiguration: configuration}})
	if err != nil || updated.Connection == nil {
		t.Fatal("apply explicit typed quiet-off configuration", err)
	}
	connection = telegramVerifyFixtureConnection(t, ctx, r, service, connection.Ref, updated.Connection.Version, "telegram-limits-rate")
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.telegram-subscription.configure", projectRef, false)
	subscription, err = service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-limits-rate-enable", ExpectedVersion: &disabled.Version}, proposal)
	if err != nil || !subscription.Enabled {
		t.Fatal("enable fresh rate scope", err)
	}
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_rate_first_fixture")
	first := telegramClaimFixture(t, ctx, r, service)
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.begin", "", true)
	if result, err := service.BeginInteractionDelivery(ctx, worker, first); err != nil || !result.Allowed {
		t.Fatal("admit first rate slot", err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.complete", "", true)
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteInteractionDelivery, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "telegram-rate-first-complete"}, Payload: command.InteractionDeliveryInput{
		DeliveryRef: first.DeliveryRef, LeaseRef: first.LeaseRef, Fence: first.Fence, Generation: first.Generation, InputDigest: first.InputDigest, Success: true, TelegramReceipt: &entity.TelegramDeliveryReceipt{BotID: 123456, ChatID: -1001234, TopicID: 77, MessageID: 501}}}); err != nil {
		t.Fatal("complete first synthetic rate slot", err)
	}
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_rate_second_fixture")
	second := telegramClaimFixture(t, ctx, r, service)
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.begin", "", true)
	admission, err = service.BeginInteractionDelivery(ctx, worker, second)
	if err != nil || admission.Allowed || admission.AlreadyStarted || admission.SafeErrorCode != "TELEGRAM_ADMISSION_DEFERRED" {
		t.Fatal("rate limit admitted a second effect", err)
	}
	if err := pool.QueryRow(ctx, telegramSubscriptionCountersFixture, subscription.Ref).Scan(&used, &rate, &version, &enabled); err != nil || used != 1 || rate != 1 || version != subscription.Version || !enabled {
		t.Fatal("rate defer consumed quota, attempts or owner version", err)
	}
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.telegram-subscription.configure", projectRef, false)
	if result, err := service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-rate-revoke", ExpectedVersion: &subscription.Version}, port.TelegramSubscriptionConfigure{ProjectRef: projectRef}); err != nil || result.Enabled {
		t.Fatal("revoke rate-deferred subscription", err)
	}
}
