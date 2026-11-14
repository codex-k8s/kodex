package platform

import (
	"context"
	_ "embed"
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

// telegramRunFixture creates one synthetic terminal event source with sensitive marker text to prove the queued snapshot excludes arbitrary Run content.
// telegramRunFixture создаёт один synthetic terminal event source с sensitive marker text, чтобы доказать исключение произвольного Run content из queued snapshot.
//
//go:embed testdata/sql/telegram_run_fixture.sql
var telegramRunFixture string

// telegramDeliveryMutationFixture attempts direct mutation of retained delivery pins and terminal receipts to exercise native database rejection.
// telegramDeliveryMutationFixture пытается напрямую изменить retained delivery pins и terminal receipts для проверки native database rejection.
//
//go:embed testdata/sql/telegram_delivery_mutation_fixture.sql
var telegramDeliveryMutationFixture string

// telegramExpireFixture advances only a disposable subscription to a valid already-expired interval without changing the host clock.
// telegramExpireFixture переводит лишь disposable subscription в допустимый истёкший интервал без изменения часов хоста.
//
//go:embed testdata/sql/telegram_expire_fixture.sql
var telegramExpireFixture string

// telegramSubscriptionClosedFixture reads durable subscription, grant and lease closure after the worker's bounded expiry transaction.
// telegramSubscriptionClosedFixture читает durable закрытие подписки, grant и lease после bounded expiry транзакции worker.
//
//go:embed testdata/sql/telegram_subscription_closed_fixture.sql
var telegramSubscriptionClosedFixture string

// telegramSubscriptionDeleteFixture attempts direct runtime deletion to prove exact project purge is the only retained subscription removal path.
// telegramSubscriptionDeleteFixture пытается напрямую удалить подписку через runtime, проверяя exact project purge как единственный путь удаления retained subscription.
//
//go:embed testdata/sql/telegram_subscription_delete_fixture.sql
var telegramSubscriptionDeleteFixture string

// telegramDeliveryClockFixture advances only disposable queue/lease timestamps so expiry and retry tests remain bounded without host clock changes or waiting.
// telegramDeliveryClockFixture продвигает лишь disposable queue/lease timestamps, сохраняя bounded expiry/retry tests без изменения часов хоста либо ожидания.
//
//go:embed testdata/sql/telegram_delivery_clock_fixture.sql
var telegramDeliveryClockFixture string

// telegramSubscriptionCountersFixture observes quota consumption and monotonic owner version independently of claim payloads.
// telegramSubscriptionCountersFixture наблюдает расход quota и монотонную owner version независимо от claim payloads.
//
//go:embed testdata/sql/telegram_subscription_counters_fixture.sql
var telegramSubscriptionCountersFixture string

// telegramEnqueueFixtureRun creates one real disposable terminal source and invokes the production owner producer twice to prove source deduplication.
// telegramEnqueueFixtureRun создаёт один реальный disposable terminal source и дважды вызывает production owner producer для проверки source deduplication.
func telegramEnqueueFixtureRun(t *testing.T, ctx context.Context, r *Repository, pool *pgxpool.Pool, current scope, projectRef, agentRef, runRef string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var runID, projectID string
	if err := tx.QueryRow(ctx, telegramRunFixture, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "project_ref": projectRef, "agent_ref": agentRef, "run_ref": runRef}).Scan(&runID, &projectID); err != nil {
		t.Fatal("seed additional Telegram source", err)
	}
	for range 2 {
		if err := r.enqueueTerminalInteractionDeliveries(ctx, tx, current, projectID, runID); err != nil {
			t.Fatal("enqueue additional Telegram source", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// telegramClaimFixture leases exactly one source through the registered service proof and constructs the corresponding immutable Begin input.
// telegramClaimFixture арендует ровно один source через registered service proof и строит соответствующий immutable Begin input.
func telegramClaimFixture(t *testing.T, ctx context.Context, r *Repository, service *serviceplatform.Service) port.InteractionDeliveryBeginInput {
	t.Helper()
	p := telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.claim", "", true)
	claims, err := service.ClaimInteractionDeliveries(ctx, p, "telegram-owner-fixture", 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim additional Telegram source: count=%d error=%v", len(claims), err)
	}
	claim := claims[0]
	return port.InteractionDeliveryBeginInput{DeliveryRef: stringMap(claim, "deliveryRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), InputDigest: stringMap(claim, "inputDigest")}
}

// telegramVerifyFixtureConnection exercises the actual queue, claim and completion of a read-only metadata probe, then reads fresh connection pins through its owner API. Provider replies remain explicitly synthetic.
// telegramVerifyFixtureConnection проходит настоящую queue, claim и completion read-only metadata probe, затем читает свежие connection pins через owner API. Provider replies остаются явно synthetic.
func telegramVerifyFixtureConnection(t *testing.T, ctx context.Context, r *Repository, service *serviceplatform.Service, connectionRef string, version int64, key string) entity.IntegrationConnection {
	t.Helper()
	p := telegramFixturePrincipal(t, ctx, r, "platform.command.integrations.test", "", false)
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: p, Mutation: value.Mutation{IdempotencyKey: key + "-test-queue", ExpectedVersion: &version}, Payload: command.ConnectionInput{Ref: connectionRef}}); err != nil {
		t.Fatal("queue synthetic Telegram health probe", err)
	}
	p = telegramFixturePrincipal(t, ctx, r, "platform.interactions.connection-tests.claim", "", true)
	tests, err := service.ClaimIntegrationConnectionTests(ctx, p, "telegram-owner-fixture", 1)
	if err != nil || len(tests) != 1 || stringMap(tests[0], "connectionRef") != connectionRef {
		t.Fatal("claim exact synthetic Telegram probe", err)
	}
	test := tests[0]
	p = telegramFixturePrincipal(t, ctx, r, "platform.interactions.connection-tests.complete", "", true)
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: p, Mutation: value.Mutation{IdempotencyKey: key + "-test-complete"}, Payload: command.IntegrationConnectionTestInput{
		TestRef: stringMap(test, "testRef"), LeaseRef: stringMap(test, "leaseRef"), Fence: stringMap(test, "fence"), Generation: test["generation"].(int64), Success: true, ResultSummary: "TELEGRAM_CONNECTION_VERIFIED"}}); err != nil {
		t.Fatal("complete synthetic Telegram metadata probe", err)
	}
	p = telegramFixturePrincipal(t, ctx, r, "platform.query.integration-connections.get", "", false)
	view, err := service.GetIntegrationConnection(ctx, p, connectionRef)
	if err != nil {
		t.Fatal("read checked Telegram connection pins", err)
	}
	return view
}

// telegramFixturePrincipal resolves fresh transport authority through the same owner proof path as existing native fixtures; no payload actor or approval is substituted.
// telegramFixturePrincipal разрешает свежие transport authority тем же owner proof path, что прежние native fixtures; payload actor либо approval не подставляются.
func telegramFixturePrincipal(t *testing.T, ctx context.Context, r *Repository, operation, project string, worker bool) value.Principal {
	t.Helper()
	input := port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: operation, ProjectRef: project}
	if worker {
		input.ExternalActorID = "kodex-system-subject"
		input.ExternalTenantID = "kodex-installation"
		input.CallerWorkload = "interaction-gateway"
	}
	if !worker {
		input.OwnerClaim = true
	}
	authority, err := r.ResolveProofAuthority(ctx, input)
	if err != nil {
		t.Fatal("resolve Telegram fixture proof authority", err)
	}
	return value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID, ProjectRef: authority.ProjectID,
		Permission: input.Operation, CorrelationRef: input.Operation + "-telegram-component", CallerWorkload: input.CallerWorkload, CredentialRevision: 1}
}

// TestTelegramSubscriptionOwnerComponent checks the actual Configure/receipt/source/claim/Begin/complete lifecycle and stale-pin/no-repeat boundaries without calling Telegram.
// TestTelegramSubscriptionOwnerComponent проверяет настоящий Configure/receipt/source/claim/Begin/complete lifecycle и stale-pin/no-repeat boundaries без обращения к Telegram.
func TestTelegramSubscriptionOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated Telegram owner database")
	}
	defer pool.Close()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	if err := r.ConfigureInteractionPublicOrigin("https://kodex.example.test"); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, r)
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	owner := telegramFixturePrincipal(t, ctx, r, "platform.command.projects.create", "", false)
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-project-create"},
		Payload: command.ProjectInput{Name: "Telegram fixture", Purpose: "Synthetic project notifications", Language: "ru"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create Telegram project: %v", err)
	}
	projectRef := project.Project.Ref
	agent := createLifecycleAgent(t, ctx, service, owner, projectRef, "telegram-agent", "Telegram fixture agent")
	connection, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-connection-create"},
		Payload: command.ConnectionInput{DefinitionKey: "telegram", Name: "Synthetic service bot", PublicConfiguration: map[string]any{
			"base_url": integrationpackage.TelegramAPIOrigin, "bot_id": float64(123456), "chat_id": "-1001234", "topic": "77", "quiet_enabled": false}}})
	if err != nil || connection.Connection == nil {
		t.Fatalf("create Telegram connection: %v", err)
	}
	if connection.Connection.PublicConfiguration["bot_id"] != "123456" || connection.Connection.PublicConfiguration["quiet_enabled"] != "false" {
		t.Fatal("browser/protobuf scalars did not retain canonical connection storage")
	}
	version := connection.Connection.Version
	configured, err := service.Execute(ctx, command.Command{Kind: command.ConfigureConnectionCredential, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "telegram-credential-configure", ExpectedVersion: &version}, Payload: command.ConnectionInput{Ref: connection.Connection.Ref, MaterializationRef: "telegram-fixture",
			CredentialRevision: &entity.IntegrationCredentialRevision{SecretRef: "kodex-system/kodex-integration-credentials#telegram-fixture", SecretUID: "11111111-1111-4111-8111-111111111111", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("d", 64)}}})
	if err != nil || configured.Connection == nil {
		t.Fatalf("configure service credential descriptor: %v", err)
	}
	version = configured.Connection.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-test-queue", ExpectedVersion: &version},
		Payload: command.ConnectionInput{Ref: connection.Connection.Ref}}); err != nil {
		t.Fatalf("queue Telegram read-only test: %v", err)
	}
	worker := telegramFixturePrincipal(t, ctx, r, "platform.interactions.connection-tests.claim", "", true)
	tests, err := service.ClaimIntegrationConnectionTests(ctx, worker, "telegram-owner-fixture", 1)
	if err != nil || len(tests) != 1 {
		t.Fatalf("claim Telegram read-only test: %v", err)
	}
	test := tests[0]
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.connection-tests.complete", "", true)
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "telegram-test-complete"},
		Payload: command.IntegrationConnectionTestInput{TestRef: stringMap(test, "testRef"), LeaseRef: stringMap(test, "leaseRef"), Fence: stringMap(test, "fence"), Generation: test["generation"].(int64), Success: true, ResultSummary: "TELEGRAM_CONNECTION_VERIFIED"}}); err != nil {
		t.Fatalf("complete synthetic Telegram metadata probe: %v", err)
	}
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.telegram-subscription.configure", projectRef, false)
	proposal := port.TelegramSubscriptionConfigure{ProjectRef: projectRef, ConnectionRef: connection.Connection.Ref, Enabled: true, EventTypes: []string{"FINISHED", "FAILED", "HUMAN_GATE"}, DurationSeconds: 86400, MaxEffects: 2}
	subscription, err := service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-subscription-enable"}, proposal)
	if err != nil || !subscription.Enabled || subscription.Version != 1 || subscription.ScopeDigest == "" {
		t.Fatalf("configure exact subscription: %v", err)
	}
	replayed, err := service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-subscription-enable"}, proposal)
	if err != nil || replayed.Ref != subscription.Ref || !replayed.ExpiresAt.Equal(subscription.ExpiresAt) {
		t.Fatal("receipt replay renewed or replaced subscription", err)
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var runID, projectID string
	if err := tx.QueryRow(ctx, telegramRunFixture, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID,
		"project_ref": projectRef, "agent_ref": agent.Ref, "run_ref": "telegram_terminal_fixture"}).Scan(&runID, &projectID); err != nil {
		t.Fatal("seed synthetic terminal source", err)
	}
	for range 2 {
		if err := r.enqueueTerminalInteractionDeliveries(ctx, tx, current, projectID, runID); err != nil {
			t.Fatal("enqueue exact terminal event", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.claim", "", true)
	claims, err := service.ClaimInteractionDeliveries(ctx, worker, "telegram-owner-fixture", 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim exact Telegram source: count=%d error=%v", len(claims), err)
	}
	claim := claims[0]
	snapshot, ok := claim["telegram"].(integrationpackage.TelegramDeliverySnapshot)
	if !ok || snapshot.SubscriptionRef != subscription.Ref || snapshot.ProjectRef != projectRef || strings.Contains(string(asJSON(snapshot)), "PRIVATE_") {
		t.Fatal("claim lost scope or leaked Run content")
	}
	begin := port.InteractionDeliveryBeginInput{DeliveryRef: stringMap(claim, "deliveryRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), InputDigest: stringMap(claim, "inputDigest")}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.begin", "", true)
	admitted, err := service.BeginInteractionDelivery(ctx, worker, begin)
	if err != nil || !admitted.Allowed {
		t.Fatalf("Begin exact Telegram effect: %v", err)
	}
	repeat, err := service.BeginInteractionDelivery(ctx, worker, begin)
	if err != nil || repeat.Allowed || !repeat.AlreadyStarted {
		t.Fatal("replayed Begin authorized a repeat", err)
	}
	wrong := begin
	wrong.InputDigest = strings.Repeat("0", 64)
	if _, err := service.BeginInteractionDelivery(ctx, worker, wrong); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign immutable input accepted", err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.complete", "", true)
	complete := command.Command{Kind: command.CompleteInteractionDelivery, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "telegram-delivery-complete"}, Payload: command.InteractionDeliveryInput{
		DeliveryRef: begin.DeliveryRef, LeaseRef: begin.LeaseRef, Fence: begin.Fence, Generation: begin.Generation, InputDigest: begin.InputDigest, Success: true,
		TelegramReceipt: &entity.TelegramDeliveryReceipt{BotID: 123456, ChatID: -1001234, TopicID: 77, MessageID: 123}}}
	for range 2 {
		result, err := service.Execute(ctx, complete)
		if err != nil || stringMap(result.Runtime, "state") != "SUCCEEDED" {
			t.Fatal("complete or replay exact Telegram receipt", err)
		}
	}
	for _, mutation := range []string{"DIGEST", "SNAPSHOT", "REQUEUE", "RECEIPT"} {
		if _, err := pool.Exec(ctx, telegramDeliveryMutationFixture, pgx.StrictNamedArgs{"delivery_ref": begin.DeliveryRef, "mutation": mutation}); err == nil {
			t.Fatal("database accepted mutable Telegram pins or terminal receipt", mutation)
		}
	}
	viewer := telegramFixturePrincipal(t, ctx, r, "platform.query.interaction-deliveries.list", projectRef, false)
	page, _, err := service.ListInteractionDeliveries(ctx, viewer, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(page) != 1 || page[0].State != "SUCCEEDED" || page[0].Retryable {
		t.Fatal("safe terminal history lost owner scope or no-repeat", err)
	}
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.telegram-subscription.configure", projectRef, false)
	proposal.MaxEffects = 100
	subscription, err = service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-subscription-replace", ExpectedVersion: &subscription.Version}, proposal)
	if err != nil || subscription.Version != 2 {
		t.Fatal("replace exact Telegram owner scope", err)
	}
	// The retained terminal source is not sent again after a new explicit subscription version.
	// Сохранённый terminal source не отправляется повторно после новой явной версии подписки.
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.enqueueTerminalInteractionDeliveries(ctx, tx, current, projectID, runID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.claim", "", true)
	if claims, err := service.ClaimInteractionDeliveries(ctx, worker, "telegram-owner-fixture", 1); err != nil || len(claims) != 0 {
		t.Fatal("scope replacement duplicated a terminal source", err)
	}
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_retry_fixture")
	retryBegin := telegramClaimFixture(t, ctx, r, service)
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.begin", "", true)
	if result, err := service.BeginInteractionDelivery(ctx, worker, retryBegin); err != nil || !result.Allowed {
		t.Fatal("admit initial rate-limited fixture", err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.complete", "", true)
	rateLimited := command.Command{Kind: command.CompleteInteractionDelivery, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "telegram-provider-429"}, Payload: command.InteractionDeliveryInput{
		DeliveryRef: retryBegin.DeliveryRef, LeaseRef: retryBegin.LeaseRef, Fence: retryBegin.Fence, Generation: retryBegin.Generation, InputDigest: retryBegin.InputDigest,
		ConfirmedNoEffect: true, SafeErrorCode: "TELEGRAM_RATE_LIMITED", RetryAfterSeconds: 1}}
	if result, err := service.Execute(ctx, rateLimited); err != nil || stringMap(result.Runtime, "state") != "FAILED" {
		t.Fatal("record confirmed no-effect rate limit", err)
	}
	var used, rate int
	var subscriptionVersion int64
	var enabled bool
	if err := pool.QueryRow(ctx, telegramSubscriptionCountersFixture, subscription.Ref).Scan(&used, &rate, &subscriptionVersion, &enabled); err != nil || used != 0 || subscriptionVersion != 2 || !enabled {
		t.Fatal("429 did not refund only its exact quota generation", err)
	}
	if _, err := pool.Exec(ctx, telegramDeliveryClockFixture, pgx.StrictNamedArgs{"delivery_ref": retryBegin.DeliveryRef, "expire_lease": false}); err != nil {
		t.Fatal(err)
	}
	freshBegin := telegramClaimFixture(t, ctx, r, service)
	if freshBegin.Generation <= retryBegin.Generation || freshBegin.Fence == retryBegin.Fence || freshBegin.LeaseRef == retryBegin.LeaseRef || freshBegin.InputDigest != retryBegin.InputDigest {
		t.Fatal("retry reused lease authority or changed immutable input")
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.begin", "", true)
	if _, err := service.BeginInteractionDelivery(ctx, worker, retryBegin); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("old retry generation remained usable", err)
	}
	if result, err := service.BeginInteractionDelivery(ctx, worker, freshBegin); err != nil || !result.Allowed {
		t.Fatal("admit bounded retry generation", err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.complete", "", true)
	unknown := command.Command{Kind: command.CompleteInteractionDelivery, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "telegram-provider-unknown"}, Payload: command.InteractionDeliveryInput{
		DeliveryRef: freshBegin.DeliveryRef, LeaseRef: freshBegin.LeaseRef, Fence: freshBegin.Fence, Generation: freshBegin.Generation, InputDigest: freshBegin.InputDigest,
		UnknownOutcome: true, SafeErrorCode: "TELEGRAM_OUTCOME_UNKNOWN"}}
	if result, err := service.Execute(ctx, unknown); err != nil || stringMap(result.Runtime, "state") != "UNKNOWN_OUTCOME" {
		t.Fatal("possible send did not remain terminal unknown", err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.claim", "", true)
	if claims, err := service.ClaimInteractionDeliveries(ctx, worker, "telegram-owner-fixture", 1); err != nil || len(claims) != 0 {
		t.Fatal("unknown send was automatically retried", err)
	}
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_lost_ack_fixture")
	lostACK := telegramClaimFixture(t, ctx, r, service)
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.begin", "", true)
	if result, err := service.BeginInteractionDelivery(ctx, worker, lostACK); err != nil || !result.Allowed {
		t.Fatal("admit lost ACK fixture", err)
	}
	if _, err := pool.Exec(ctx, telegramDeliveryClockFixture, pgx.StrictNamedArgs{"delivery_ref": lostACK.DeliveryRef, "expire_lease": true}); err != nil {
		t.Fatal(err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.claim", "", true)
	if claims, err := service.ClaimInteractionDeliveries(ctx, worker, "telegram-owner-fixture", 1); err != nil || len(claims) != 0 {
		t.Fatal("expired possible-effect lease was retried", err)
	}
	viewer = telegramFixturePrincipal(t, ctx, r, "platform.query.interaction-deliveries.list", projectRef, false)
	page, _, err = service.ListInteractionDeliveries(ctx, viewer, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(page) != 3 {
		t.Fatal("bounded Telegram history lost additional outcomes", err)
	}
	unknowns := 0
	for _, item := range page {
		if item.State == "UNKNOWN_OUTCOME" {
			unknowns++
			if item.Retryable {
				t.Fatal("unknown history advertised retry")
			}
		}
	}
	if unknowns != 2 {
		t.Fatal("lost completion ACK or unknown provider outcome was not retained")
	}
	owner = telegramFixturePrincipal(t, ctx, r, "platform.query.integration-connections.get", "", false)
	currentConnection, err := service.GetIntegrationConnection(ctx, owner, connection.Connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	dependencyTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lockedDependency, err := r.lockIntegrationConnection(ctx, dependencyTx, current.organizationID, currentConnection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	strictEffects, _, err := r.integrationConnectionDependencies(ctx, dependencyTx, lockedDependency.id, false)
	if err != nil || strictEffects != 2 {
		t.Fatal("policy/package/delete unknown-outcome guard was relaxed", err)
	}
	editableEffects, _, err := r.integrationConnectionDependencies(ctx, dependencyTx, lockedDependency.id, true)
	if err != nil || editableEffects != 0 {
		t.Fatal("closed Telegram unknown remained an ongoing config effect", err)
	}
	if err := dependencyTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.integrations.update", "", false)
	updated, err := service.Execute(ctx, command.Command{Kind: command.UpdateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-config-after-unknown", ExpectedVersion: &currentConnection.Version}, Payload: command.ConnectionInput{
		Ref: currentConnection.Ref, Name: "Updated synthetic service bot", PublicConfiguration: map[string]any{"base_url": integrationpackage.TelegramAPIOrigin, "bot_id": float64(123456), "chat_id": "-1001234", "topic": "77", "quiet_enabled": false, "queue_limit": float64(32)}}})
	if err != nil || updated.Connection == nil || updated.Connection.Version <= currentConnection.Version {
		t.Fatal("explicit Telegram config update could not retain terminal unknown history", err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.complete", "", true)
	staleSuccess := command.Command{Kind: command.CompleteInteractionDelivery, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "telegram-stale-success-after-config"}, Payload: command.InteractionDeliveryInput{
		DeliveryRef: freshBegin.DeliveryRef, LeaseRef: freshBegin.LeaseRef, Fence: freshBegin.Fence, Generation: freshBegin.Generation, InputDigest: freshBegin.InputDigest, Success: true, TelegramReceipt: &entity.TelegramDeliveryReceipt{BotID: 123456, ChatID: -1001234, TopicID: 77, MessageID: 999}}}
	if _, err := service.Execute(ctx, staleSuccess); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("stale completion restored terminal unknown to success", err)
	}
	viewer = telegramFixturePrincipal(t, ctx, r, "platform.query.telegram-subscription.get", projectRef, false)
	disabled, canManage, err := service.GetTelegramSubscription(ctx, viewer, projectRef)
	if err != nil || disabled == nil || disabled.Enabled || !canManage || disabled.Version <= subscription.Version {
		t.Fatal("configuration change retained old subscription authority", err)
	}
	checked := telegramVerifyFixtureConnection(t, ctx, r, service, currentConnection.Ref, updated.Connection.Version, "telegram-updated")
	if checked.State != "CONNECTED" {
		t.Fatal("updated connection is not checked")
	}
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.telegram-subscription.configure", projectRef, false)
	subscription, err = service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-new-approved-config-scope", ExpectedVersion: &disabled.Version}, proposal)
	if err != nil || !subscription.Enabled {
		t.Fatal("configure fresh checked owner scope", err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.enqueueTerminalInteractionDeliveries(ctx, tx, current, projectID, runID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.claim", "", true)
	if claims, err := service.ClaimInteractionDeliveries(ctx, worker, "telegram-owner-fixture", 1); err != nil || len(claims) != 0 {
		t.Fatal("new config scope duplicated the retained terminal source", err)
	}
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_future_new_scope_fixture")
	future := telegramClaimFixture(t, ctx, r, service)
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.begin", "", true)
	if result, err := service.BeginInteractionDelivery(ctx, worker, future); err != nil || !result.Allowed {
		t.Fatal("new real source was unavailable in newly approved scope", err)
	}
	testTelegramLimitsComponent(t, ctx, pool, r, service, projectRef)
	testTelegramConcurrentAdmissionComponent(t, ctx, pool, r, service)
	testTelegramNativeGateComponent(t)
	testTelegramApproverAuthorityComponent(t, ctx, pool, r, service)
	testTelegramManagedHealthOnlyComponent(t, ctx, r, service, projectRef)
	if _, err := pool.Exec(ctx, telegramExpireFixture, subscription.Ref); err != nil {
		t.Fatal("expire synthetic subscription", err)
	}
	worker = telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.claim", "", true)
	if claims, err := service.ClaimInteractionDeliveries(ctx, worker, "telegram-owner-fixture", 1); err != nil || len(claims) != 0 {
		t.Fatal("expired subscription admitted another effect", err)
	}
	var closed bool
	if err := pool.QueryRow(ctx, telegramSubscriptionClosedFixture, subscription.Ref).Scan(&closed); err != nil || !closed {
		t.Fatal("subscription expiry did not close grant and leases", err)
	}
	if _, err := pool.Exec(ctx, telegramSubscriptionDeleteFixture, subscription.Ref); err == nil {
		t.Fatal("runtime deleted retained subscription outside exact purge")
	}
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.projects.trash", projectRef, false)
	trashed, err := service.Execute(ctx, command.Command{Kind: command.TrashProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-project-trash", ExpectedVersion: &project.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: projectRef}})
	if err != nil || trashed.Project == nil {
		t.Fatal("trash expired Telegram project", err)
	}
	// Purge resolves the trashed resource inside its dedicated owner command, matching the gateway's root-scoped operation profile.
	// Purge разрешает удалённый ресурс внутри специальной owner command, как root-scoped operation profile gateway.
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.projects.purge", "", false)
	if _, err := service.Execute(ctx, command.Command{Kind: command.PurgeProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-project-purge", ExpectedVersion: &trashed.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: projectRef}}); err != nil {
		t.Fatal("request exact Telegram project purge", err)
	}
	candidates, err := r.ListPendingProjectPurges(ctx, 4)
	if err != nil || len(candidates) != 1 || candidates[0].ProjectRef != projectRef {
		t.Fatal("Telegram project purge candidate missing", err)
	}
	inventory, err := r.ProjectPurgeInventory(ctx, candidates[0])
	if err != nil || len(inventory) != 0 {
		t.Fatal("synthetic Telegram project has unexpected external obligations", err)
	}
	if err := r.FinalizeProjectPurge(ctx, candidates[0], inventory); err != nil {
		t.Fatal("purge exact Telegram project owner graph", err)
	}
	if err := pool.QueryRow(ctx, telegramSubscriptionClosedFixture, subscription.Ref).Scan(&closed); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("purge retained Telegram subscription or project grant", err)
	}
}
