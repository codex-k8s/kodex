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
	// queryWorkspaceStorageLegacyEligible locks the exact trusted session and proves absence of every V2 row, independent of candidate agent.
	// queryWorkspaceStorageLegacyEligible блокирует exact trusted session и доказывает отсутствие любых V2 rows независимо от candidate agent.
	//go:embed sql/workspace_storage__legacy_eligible.sql
	queryWorkspaceStorageLegacyEligible string
	// queryWorkspaceStorageSession locks the exact session even before its first work row exists.
	// queryWorkspaceStorageSession блокирует exact session даже до появления первой work row.
	//go:embed sql/workspace_storage__session.sql
	queryWorkspaceStorageSession string
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
	activated := repository.roleImages.WorkspaceStorageProfile == runtimecontract.WorkspaceStorageProfile
	if !activated {
		var legacyEligible bool
		err := tx.QueryRow(ctx, queryWorkspaceStorageLegacyEligible, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "session_id": candidate.sessionID,
			"project_id": candidate.projectID}).Scan(&legacyEligible)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, errs.ErrConflict
		}
		if err != nil {
			return nil, false, errs.ErrUnavailable
		}
		if legacyEligible {
			return nil, true, nil
		}
	}
	arguments := pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_id": candidate.sessionID,
		"project_id": candidate.projectID, "agent_id": candidate.agentID}
	var sessionID string
	if err := tx.QueryRow(ctx, queryWorkspaceStorageSession, arguments).Scan(&sessionID); err != nil || sessionID != candidate.sessionID {
		return nil, false, errs.ErrConflict
	}
	var reference, state string
	var generation int64
	err := tx.QueryRow(ctx, queryWorkspaceStorageLock, arguments).Scan(&reference, &generation, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		if !activated {
			// Единственный legacy-допуск уже проверил отсутствие любой V2 row.
			return nil, false, errs.ErrConflict
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
	ready, volumeReady, err := repository.prepareWorkspaceVolumeTx(ctx, tx, current, candidate, *binding)
	if err != nil || !volumeReady {
		return nil, false, err
	}
	binding.VolumeReady = ready
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
