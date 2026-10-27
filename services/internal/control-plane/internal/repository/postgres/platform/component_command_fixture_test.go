package platform

import (
	"context"
	"testing"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// newComponentCommandExecutor returns a test-only command executor with the caller's exact
// idempotency prefix and expected version. Errors fail the test with the original kind/key context;
// it does not choose authority or alter command payloads.
// newComponentCommandExecutor возвращает тестовый исполнитель команд с точным префиксом
// идемпотентности и ожидаемой версией caller. Ошибки завершают тест с исходным kind/key;
// helper не выбирает authority и не меняет payload команд.
func newComponentCommandExecutor(t *testing.T, ctx context.Context, service *platformservice.Service, prefix string) func(command.Kind, value.Principal, string, *int64, any) command.Result {
	return func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: prefix + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
}

// openComponentScopeTransaction resolves the test principal and opens the same default transaction.
// The caller owns rollback, access checks and any fixture effects; this helper grants no authority.
// openComponentScopeTransaction разрешает test principal и открывает прежнюю default transaction.
// Rollback, проверка доступа и эффекты fixture принадлежат caller; helper не выдаёт полномочий.
func openComponentScopeTransaction(t *testing.T, ctx context.Context, repository *Repository, owner value.Principal) (scope, pgx.Tx) {
	t.Helper()
	principal, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return current, tx
}
