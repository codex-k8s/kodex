package platform

import (
	"context"
	"testing"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
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
