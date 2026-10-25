package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryConfigurationChangeintegrationgrantPolicyChangeActive embeds detection of WAITING_APPROVAL,
// READY, RUNNING or UNKNOWN_OUTCOME invocations for an exact grant target and capability. The
// caller uses it to prevent changing approval policy under an existing effect.
// queryConfigurationChangeintegrationgrantPolicyChangeActive встраивает поиск WAITING_APPROVAL,
// READY, RUNNING либо UNKNOWN_OUTCOME invocation точных цели grant и capability. Вызывающий код
// использует его, чтобы не менять политику одобрения под существующим эффектом.
//
//go:embed sql/configuration_changeintegrationgrant_policy_change_active.sql
var queryConfigurationChangeintegrationgrantPolicyChangeActive string

// validIntegrationApprovalPolicy accepts only NONE, HUMAN_EACH_EFFECT or HUMAN_SCOPED. This syntax
// predicate does not prove that a capability allows the policy or that scoped paths are valid.
// validIntegrationApprovalPolicy принимает только NONE, HUMAN_EACH_EFFECT либо HUMAN_SCOPED.
// Проверка формы не доказывает, что capability допускает политику или что scoped-пути корректны.
func validIntegrationApprovalPolicy(policy string) bool {
	return policy == "NONE" || policy == "HUMAN_EACH_EFFECT" || policy == "HUMAN_SCOPED"
}

// requireIntegrationGrantPolicyChangeIdle skips disabled payloads; otherwise checks the exact
// agent/workflow target and capability for active or unknown-outcome invocations. Query failure is
// Unavailable and any match Conflict; it does not reconcile unknown outcomes, mutate policy or
// commit.
// requireIntegrationGrantPolicyChangeIdle пропускает выключенные payload; иначе проверяет точные
// цель agent/workflow и capability на активные invocation или неизвестный исход. Ошибка запроса
// даёт Unavailable, совпадение — Conflict; не разрешает UNKNOWN_OUTCOME, не меняет политику и не
// завершает транзакцию.
func requireIntegrationGrantPolicyChangeIdle(ctx context.Context, tx pgx.Tx, organizationID, connectionID string, payload command.IntegrationGrantInput) error {
	if !payload.Enabled {
		return nil
	}
	targetKind, targetRef := "AGENT", payload.AgentRef
	if payload.WorkflowRef != "" {
		targetKind, targetRef = "WORKFLOW", payload.WorkflowRef
	}
	var active bool
	if err := tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantPolicyChangeActive,
		organizationID, connectionID, payload.CapabilityKey, targetKind, targetRef).Scan(&active); err != nil {
		return errs.ErrUnavailable
	}
	if active {
		return errs.ErrConflict
	}
	return nil
}
