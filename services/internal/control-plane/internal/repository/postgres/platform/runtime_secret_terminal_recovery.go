package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// terminalRuntimeSecretRecovery admits only immutable completed CREATE/ROTATE operations to the fresh-retention transaction; active completion and other operation kinds retain their original path.
// terminalRuntimeSecretRecovery допускает к транзакции свежего retention лишь immutable completed CREATE/ROTATE operations; active completion и прочие виды сохраняют прежний путь.
func terminalRuntimeSecretRecovery(operation lockedRuntimeSecretOperation) bool {
	return operation.state == "COMPLETED" && (operation.kind == "CREATE" || operation.kind == "ROTATE")
}

// recoverTerminalRuntimeSecret uses READ COMMITTED only for an authoritatively locked terminal operation. The Secret lock precedes a separate fresh retention statement, so a committed admission winning the lock race is visible. The nonterminal probe rolls back before the unchanged SERIALIZABLE path, and storage/auth failures never become fallback or DELETE. Legacy recovery has no caller claimant/generation fields; existing operation/materialization checks remain authoritative.
// recoverTerminalRuntimeSecret использует READ COMMITTED лишь для авторитетно заблокированной terminal operation. Secret lock предшествует отдельному свежему retention statement, поэтому победивший admission commit виден после ожидания. Nonterminal probe откатывается до прежнего SERIALIZABLE path, а storage/auth failures не превращаются в fallback либо DELETE. Legacy recovery не имеет caller claimant/generation fields; сохраняются существующие operation/materialization checks.
func (repository *Repository) recoverTerminalRuntimeSecret(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretRecoveryInput) (platformrepo.RuntimeSecretRecoveryResult, bool, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, false, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, false, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := repository.lockRuntimeSecretOperation(ctx, tx, current.organizationID, input.OperationRef)
	if err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, false, err
	}
	if !terminalRuntimeSecretRecovery(locked) {
		if err := tx.Rollback(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, false, errs.ErrUnavailable
		}
		return platformrepo.RuntimeSecretRecoveryResult{}, false, nil
	}
	result := platformrepo.RuntimeSecretRecoveryResult{Action: "DELETE", OperationState: locked.state}
	if repository.materializationMatchesOperation(locked, &input.Materialization) {
		descriptor, descriptorErr := repository.runtimeSecretRevision(ctx, tx, locked.secretID, locked.targetRevision)
		if descriptorErr != nil && !errors.Is(descriptorErr, errs.ErrNotFound) {
			return platformrepo.RuntimeSecretRecoveryResult{}, true, descriptorErr
		}
		var retained bool
		if err := tx.QueryRow(ctx, queryRuntimeSecretRevisionRetained, current.organizationID, locked.secretID, locked.targetRevision).Scan(&retained); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, true, errs.ErrUnavailable
		}
		if descriptorErr == nil && runtimeSecretMaterializationMatchesDescriptor(input.Materialization, descriptor) && locked.secretVersion >= locked.expectedSecretVersion && retained {
			secret, err := decodeRuntimeSecretSnapshot(locked.terminalSnapshot)
			if err != nil {
				return platformrepo.RuntimeSecretRecoveryResult{}, true, errs.ErrUnavailable
			}
			result.Action, result.Secret = "KEEP", &secret
		}
		if !retained && descriptorErr == nil && runtimeSecretMaterializationMatchesDescriptor(input.Materialization, descriptor) {
			if _, err := tx.Exec(ctx, queryRuntimeSecretRetireRevision, locked.secretID, locked.targetRevision); err != nil {
				return platformrepo.RuntimeSecretRecoveryResult{}, true, errs.ErrUnavailable
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, true, errs.ErrConflict
	}
	return result, true, nil
}
