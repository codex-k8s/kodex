package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWorkspaceStorageExists detects the additive schema without referencing absent legacy columns.
	// queryWorkspaceStorageExists определяет additive schema без обращения к отсутствующим legacy columns.
	//go:embed sql/workspace_storage__exists.sql
	queryWorkspaceStorageExists string
	// queryWorkspaceStorageLock serializes the existing session and exact owned work row.
	// queryWorkspaceStorageLock сериализует existing session и exact owned work row.
	//go:embed sql/workspace_storage__lock.sql
	queryWorkspaceStorageLock string
	// queryWorkspaceStorageCreate creates data identity only after legacy destructive tasks are drained.
	// queryWorkspaceStorageCreate создаёт data identity только после drain legacy destructive tasks.
	//go:embed sql/workspace_storage__create.sql
	queryWorkspaceStorageCreate string
	// queryWorkspaceStorageClaim binds one newly issued immutable lease before any runtime materialization.
	// queryWorkspaceStorageClaim связывает один новый immutable lease до runtime materialization.
	//go:embed sql/workspace_storage__claim.sql
	queryWorkspaceStorageClaim string
)

// prepareSessionWorkspaceTx preserves default legacy and never falls back for existing V2 data.
// Pending/unknown work stays unclaimable; returning no work does not roll back durable preparation.
// prepareSessionWorkspaceTx сохраняет default legacy без fallback для existing V2 data.
// Pending/unknown work не допускается к claim; отсутствие работы не откатывает durable preparation.
func (repository *Repository) prepareSessionWorkspaceTx(ctx context.Context, tx pgx.Tx, current scope, candidate claimableExecution) (*runtimecontract.WorkspaceStorageBinding, bool, error) {
	if runtimeAssistantScope(candidate.stableKey) != runtimecontract.AssistantScopeNone {
		return nil, true, nil
	}
	var schema bool
	if err := tx.QueryRow(ctx, queryWorkspaceStorageExists).Scan(&schema); err != nil {
		return nil, false, errs.ErrUnavailable
	}
	activated := repository.roleImages.WorkspaceStorageProfile == runtimecontract.WorkspaceStorageProfile
	if !schema {
		if activated {
			return nil, false, errs.ErrUnavailable
		}
		return nil, true, nil
	}
	arguments := pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_id": candidate.sessionID,
		"project_id": candidate.projectID, "agent_id": candidate.agentID}
	var reference, state string
	var generation int64
	err := tx.QueryRow(ctx, queryWorkspaceStorageLock, arguments).Scan(&reference, &generation, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		if !activated {
			return nil, true, nil
		}
		reference, err = newRef("wks")
		if err != nil {
			return nil, false, err
		}
		arguments["workspace_ref"] = reference
		err = tx.QueryRow(ctx, queryWorkspaceStorageCreate, arguments).Scan(&reference, &generation, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
	}
	if err != nil {
		return nil, false, errs.ErrUnavailable
	}
	if !activated || state != "EMPTY" {
		// Остальные состояния разрешает только owner checkpoint/restore coordinator.
		return nil, false, nil
	}
	binding := &runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile,
		Scope: runtimecontract.WorkspaceStorageScope{OrganizationRef: candidate.organizationRef, ProjectRef: candidate.projectRef,
			SessionRef: candidate.sessionRef, AgentRef: candidate.agentRef}, WorkspaceRef: reference, Generation: generation}
	if _, err := binding.GenerationSubPath(); err != nil {
		return nil, false, errs.ErrConflict
	}
	return binding, true, nil
}

// claimSessionWorkspaceTx assigns the new lease using the same owner transaction that issued its RR.
// It never steals an existing writer or upgrades a checkpoint without a verified restore transition.
// claimSessionWorkspaceTx назначает новый lease в owner transaction, создавшей его RR.
// Existing writer не отбирается, checkpoint не повышается без verified restore transition.
func claimSessionWorkspaceTx(ctx context.Context, tx pgx.Tx, current scope, binding *runtimecontract.WorkspaceStorageBinding, leaseRef string) error {
	if binding == nil {
		return nil
	}
	var version int64
	if err := tx.QueryRow(ctx, queryWorkspaceStorageClaim, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "workspace_ref": binding.WorkspaceRef,
		"workspace_generation": binding.Generation, "lease_ref": leaseRef,
	}).Scan(&version); err != nil {
		return errs.ErrConflict
	}
	return nil
}
