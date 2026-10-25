package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantGrantConnection returns a connection ref only for the three recognized grant operation
// kinds with an exact INTEGRATION_CONNECTION target/parameter match. Empty output means this
// helper cannot carry a connection version; it does not authorize the operation.
// assistantGrantConnection возвращает ссылку подключения только для трёх известных видов
// grant-операций при точном совпадении цели INTEGRATION_CONNECTION и параметра. Пустой результат
// означает невозможность переноса версии этим helper; полномочия операции не проверяются.
func assistantGrantConnection(operation entity.AssistantPlanOperation) string {
	if operation.Type != "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" && operation.Type != "CHANGE_INTEGRATION_GRANT" && operation.Type != changeProjectAssistantIntegrationGrant {
		return ""
	}
	if operation.Target.Kind != "INTEGRATION_CONNECTION" || operation.Target.Ref != assistantString(operation.Parameters, "connectionRef") {
		return ""
	}
	return operation.Target.Ref
}

// carryAssistantGrantConnectionVersion leaves invalid versions or unrecognized connection
// operations unchanged. Otherwise it clones Input and updates its expectedVersion plus independent
// expected/target version pointers from a prior canonical effect in the same owner transaction;
// immutable source snapshots remain unchanged.
// carryAssistantGrantConnectionVersion оставляет неверную версию либо неизвестную операцию
// подключения без изменения. Иначе копирует Input и обновляет expectedVersion и отдельные
// указатели ожидаемой и целевой версии из предыдущего канонического эффекта той же транзакции
// владельца; неизменяемые исходные снимки сохраняются.
func carryAssistantGrantConnectionVersion(operation entity.AssistantPlanOperation, version int64) entity.AssistantPlanOperation {
	if version < 1 || assistantGrantConnection(operation) == "" {
		return operation
	}
	operation.Input = cloneAssistantFields(operation.Input)
	operation.Input["expectedVersion"] = version
	expected, target := version, version
	operation.ExpectedVersion, operation.Target.Version = &expected, &target
	return operation
}

// preflightAssistantGrantPlan authorizes selected recognized grant operations and checks their
// specialized current snapshots before effects. Conversion/authorization failures propagate; a
// snapshot error or mismatch returns a copy of the offending operation with nil error for
// stale-plan handling. No match returns nil,nil and performs no write.
// preflightAssistantGrantPlan авторизует выбранные известные grant-операции и проверяет их
// специализированные текущие снимки до эффектов. Ошибки преобразования и авторизации передаются
// выше; ошибка либо несовпадение снимка возвращает копию проблемной операции с nil error для
// обработки устаревшего плана. Отсутствие проблемы даёт nil,nil без записи.
func (repository *Repository) preflightAssistantGrantPlan(ctx context.Context, tx pgx.Tx, current scope, projectRef string, operations []entity.AssistantPlanOperation) (*entity.AssistantPlanOperation, error) {
	for _, operation := range operations {
		if !operation.Selected || assistantGrantConnection(operation) == "" {
			continue
		}
		planned, err := assistantOperationCommand(operation)
		if err != nil {
			return nil, err
		}
		if err = repository.authorizeCommand(ctx, tx, current, planned); err != nil {
			return nil, err
		}
		var matching bool
		if operation.Type == changeProjectAssistantIntegrationGrant {
			matching, err = repository.projectAssistantIntegrationGrantSnapshotMatches(ctx, tx, current, operation)
		} else if operation.Type == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
			matching, err = repository.systemAssistantIntegrationGrantSnapshotMatches(ctx, tx, current, operation)
		} else {
			matching, err = repository.assistantIntegrationGrantSnapshotMatches(ctx, tx, current, projectRef, operation)
		}
		if err != nil || !matching {
			copy := operation
			return &copy, nil
		}
	}
	return nil, nil
}
