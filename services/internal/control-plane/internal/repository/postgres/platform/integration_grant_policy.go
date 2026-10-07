package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/configuration_changeintegrationgrant_policy_change_active.sql
var queryConfigurationChangeintegrationgrantPolicyChangeActive string

func validIntegrationApprovalPolicy(policy string) bool {
	return policy == "NONE" || policy == "HUMAN_EACH_EFFECT" || policy == "HUMAN_SCOPED"
}

// Policy/path не заменяются под уже начатым effect. UNKNOWN_OUTCOME требует reconciliation.
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
