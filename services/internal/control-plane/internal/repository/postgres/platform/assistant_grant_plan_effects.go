package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func assistantGrantConnection(operation entity.AssistantPlanOperation) string {
	if operation.Type != "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" && operation.Type != "CHANGE_INTEGRATION_GRANT" {
		return ""
	}
	if operation.Target.Kind != "INTEGRATION_CONNECTION" || operation.Target.Ref != assistantString(operation.Parameters, "connectionRef") {
		return ""
	}
	return operation.Target.Ref
}

// Переносится только версия точного connection, возвращённая предыдущим
// каноническим grant-effect этой owner-транзакции. Immutable source не меняется.
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
		if operation.Type == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
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
