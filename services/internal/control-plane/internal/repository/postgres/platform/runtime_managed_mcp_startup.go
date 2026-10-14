package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// Private intent приходит только из server-resolved exact startup candidate.
// Он не является readiness или разрешением на probe/lease/provider execution.
type managedMCPStartupRecovery struct {
	scopeKind, scopeRef, agentRef, projectRef string
	grants                                    []runtimecontract.RunnerIntegrationGrant
}

func (*managedMCPStartupRecovery) Error() string {
	return "managed MCP startup health refresh is required"
}

//go:embed sql/runtime_managed_mcp__startup_lock.sql
var queryRuntimeManagedMCPStartupLock string

// CP-owned ClaimExecution является вторым точным origin maintenance DUE,
// только для stale latest success с неизменившимися pins. Gateway остаётся
// единственным probe claimant/executor. Этот effect сохраняется после rollback
// candidate materialization и атомарно с runtime command audit/receipt.
func enqueueManagedMCPStartupRecovery(ctx context.Context, tx pgx.Tx, current scope, recovery *managedMCPStartupRecovery) (bool, error) {
	if recovery == nil {
		return false, errs.ErrInvalid
	}
	resolve, _, valid := managedMCPPendingGrantPair(recovery.grants)
	if !valid {
		return false, errs.ErrConflict
	}
	var connectionID string
	err := tx.QueryRow(ctx, queryRuntimeManagedMCPStartupLock, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "connection_ref": resolve.ConnectionRef, "connection_version": resolve.ConnectionVersion,
	}).Scan(&connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errs.ErrConflict
	}
	if err != nil {
		return false, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if err := requireManagedMCPStartupDependencies(ctx, tx, current.organizationID, recovery.agentRef, recovery.grants); err != nil {
		return false, err
	}
	profiles, err := runtimeManagedMCPProfilesWithHealthQuery(ctx, tx, current.organizationID, recovery.scopeKind, recovery.scopeRef, recovery.agentRef, recovery.projectRef, recovery.grants, queryRuntimeManagedMCPStale, false)
	// Другой owner уже поставил probe или сохранил fresh receipt: никакого reset.
	if errors.Is(err, errs.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(profiles) != 1 {
		return false, errs.ErrConflict
	}
	ref, err := newRef("tst")
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, queryIntegrationHealthRefreshCreate, pgx.StrictNamedArgs{
		"ref": ref, "organization_id": current.organizationID, "connection_id": connectionID,
		"actor_id": current.actorID, "attempt": 1, "predecessor_ref": profiles[0].Health.TestRef,
	}); err != nil {
		return false, serializableTransactionError(err, errs.ErrUnavailable)
	}
	return true, nil
}
