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

// telegramConcurrentAdmissionResult carries a goroutine's owner decision back to the test without invoking testing fatal methods outside the test goroutine.
// telegramConcurrentAdmissionResult передаёт решение владельца из goroutine в тест, не вызывая testing fatal methods вне goroutine теста.
type telegramConcurrentAdmissionResult struct {
	admission entity.InteractionDeliveryBegin
	err       error
}

// testTelegramConcurrentAdmissionComponent proves duplicate concurrent Begin reserves exactly one quota slot and concurrent revoke closes every lease without turning a possibly started effect into a retryable item.
// testTelegramConcurrentAdmissionComponent доказывает, что конкурентные Begin резервируют ровно один quota slot, а конкурентный revoke закрывает все leases, не делая возможно начатый эффект повторяемым.
func testTelegramConcurrentAdmissionComponent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, r *Repository, service *serviceplatform.Service) {
	t.Helper()
	owner := telegramFixturePrincipal(t, ctx, r, "platform.command.projects.create", "", false)
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "telegram-concurrent-project"}, Payload: command.ProjectInput{Name: "Telegram concurrency fixture", Purpose: "Synthetic admission and revoke serialization", Language: "ru"}})
	if err != nil || project.Project == nil {
		t.Fatal("create concurrent Telegram project", err)
	}
	projectRef := project.Project.Ref
	agent := createLifecycleAgent(t, ctx, service, owner, projectRef, "telegram-concurrent-agent", "Telegram concurrency agent")
	connection := telegramCreateFixtureConnection(t, ctx, r, service, map[string]any{"base_url": integrationpackage.TelegramAPIOrigin, "bot_id": float64(123456), "chat_id": "-1001234", "topic": "77", "quiet_enabled": false}, "telegram-concurrent-fixture")
	owner = telegramFixturePrincipal(t, ctx, r, "platform.command.telegram-subscription.configure", projectRef, false)
	subscription, err := service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-concurrent-enable"}, port.TelegramSubscriptionConfigure{ProjectRef: projectRef, ConnectionRef: connection.Ref, Enabled: true, EventTypes: []string{"FINISHED"}, DurationSeconds: 3600, MaxEffects: 4})
	if err != nil {
		t.Fatal("enable concurrent Telegram subscription", err)
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_concurrent_begin_fixture")
	first := telegramClaimFixture(t, ctx, r, service)
	worker := telegramFixturePrincipal(t, ctx, r, "platform.interactions.deliveries.begin", "", true)
	start := make(chan struct{})
	results := make(chan telegramConcurrentAdmissionResult, 2)
	for range 2 {
		go func() {
			<-start
			admission, err := service.BeginInteractionDelivery(ctx, worker, first)
			results <- telegramConcurrentAdmissionResult{admission: admission, err: err}
		}()
	}
	close(start)
	allowed := 0
	for range 2 {
		answer := <-results
		if answer.err != nil && !errors.Is(answer.err, errs.ErrConflict) && !errors.Is(answer.err, errs.ErrUnavailable) {
			t.Fatal("concurrent Begin returned an unexpected owner error", answer.err)
		}
		if answer.admission.Allowed {
			if answer.err != nil || answer.admission.AlreadyStarted {
				t.Fatal("failed concurrent Begin retained send permission")
			}
			allowed++
		}
	}
	if allowed != 1 {
		t.Fatalf("concurrent Begin admitted %d effects, expected one", allowed)
	}
	if replay, err := service.BeginInteractionDelivery(ctx, worker, first); err != nil || replay.Allowed || !replay.AlreadyStarted {
		t.Fatal("fresh Begin replay authorized another concurrent effect", err)
	}
	var used, rate int
	var version int64
	var enabled bool
	if err := pool.QueryRow(ctx, telegramSubscriptionCountersFixture, subscription.Ref).Scan(&used, &rate, &version, &enabled); err != nil || used != 1 || rate != 1 || !enabled || version != subscription.Version {
		t.Fatal("concurrent Begin consumed more than one quota slot", err)
	}
	telegramEnqueueFixtureRun(t, ctx, r, pool, current, projectRef, agent.Ref, "telegram_concurrent_revoke_fixture")
	second := telegramClaimFixture(t, ctx, r, service)
	start = make(chan struct{})
	revoke := make(chan error, 1)
	go func() {
		<-start
		admission, err := service.BeginInteractionDelivery(ctx, worker, second)
		results <- telegramConcurrentAdmissionResult{admission: admission, err: err}
	}()
	go func() {
		<-start
		_, err := service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-concurrent-revoke", ExpectedVersion: &subscription.Version}, port.TelegramSubscriptionConfigure{ProjectRef: projectRef})
		revoke <- err
	}()
	close(start)
	answer := <-results
	if answer.err != nil && !errors.Is(answer.err, errs.ErrConflict) && !errors.Is(answer.err, errs.ErrUnavailable) {
		t.Fatal("Begin racing revoke returned an unexpected owner error", answer.err)
	}
	revokeErr := <-revoke
	if revokeErr != nil {
		if !errors.Is(revokeErr, errs.ErrConflict) && !errors.Is(revokeErr, errs.ErrUnavailable) {
			t.Fatal("concurrent revoke returned an unexpected owner error", revokeErr)
		}
		// An explicit retry preserves the exact owner intent and idempotency key after a transaction conflict; it does not infer approval or repeat an external effect.
		// Явный повтор сохраняет точное owner intent и idempotency key после transaction conflict; он не выводит approval и не повторяет внешний эффект.
		if _, err := service.ConfigureTelegramSubscription(ctx, owner, value.Mutation{IdempotencyKey: "telegram-concurrent-revoke", ExpectedVersion: &subscription.Version}, port.TelegramSubscriptionConfigure{ProjectRef: projectRef}); err != nil {
			t.Fatal("retry exact owner revoke after transaction conflict", err)
		}
	}
	for _, input := range []port.InteractionDeliveryBeginInput{first, second} {
		if admission, err := service.BeginInteractionDelivery(ctx, worker, input); !errors.Is(err, errs.ErrConflict) || admission.Allowed || admission.AlreadyStarted {
			t.Fatal("revocation left a live admission or replay lease", err)
		}
	}
	var closed bool
	if err := pool.QueryRow(ctx, telegramSubscriptionClosedFixture, subscription.Ref).Scan(&closed); err != nil || !closed {
		t.Fatal("concurrent revoke retained live grant or lease", err)
	}
	viewer := telegramFixturePrincipal(t, ctx, r, "platform.query.interaction-deliveries.list", projectRef, false)
	history, _, err := service.ListInteractionDeliveries(ctx, viewer, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(history) != 2 {
		t.Fatal("concurrent revoke lost retained delivery history", err)
	}
	for _, delivery := range history {
		expected := "UNKNOWN_OUTCOME"
		if delivery.Ref == second.DeliveryRef && !answer.admission.Allowed {
			expected = "CANCELLED"
		}
		if delivery.State != expected {
			t.Fatalf("revocation changed possible-send semantics: state=%s expected=%s", delivery.State, expected)
		}
	}
}
