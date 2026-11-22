package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesSetupCancelDependency closes setup/attempt authority while preserving material history.
// queryKubernetesSetupCancelDependency закрывает setup/attempt authority, сохраняя material history.
//
//go:embed sql/kubernetes_setup__cancel_dependency.sql
var queryKubernetesSetupCancelDependency string

// cancelKubernetesIdentitySetupsTx fences the exact locked Connection or personal binding dependency.
// No source lock is acquired after the Connection/binding lock; this closing-only transition cannot
// admit a new grant. It retains UNKNOWN receipts and makes no assertion that an external CREATE stopped.
// cancelKubernetesIdentitySetupsTx закрывает exact locked Connection либо personal binding dependency.
// После Connection/binding lock не берётся source lock; closing-only transition не создаёт grants.
// UNKNOWN receipts сохраняются, остановка внешнего CREATE не утверждается.
func cancelKubernetesIdentitySetupsTx(ctx context.Context, tx pgx.Tx, organizationID, connectionRef, bindingID string) error {
	if _, err := tx.Exec(ctx, queryKubernetesSetupCancelDependency, pgx.StrictNamedArgs{
		"organization_id": organizationID, "connection_ref": connectionRef, "binding_id": bindingID,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
