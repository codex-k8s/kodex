package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// sshExecutionAdmissionTx exposes only nested admission and rollback so an unexpected owner query or commit fails instead of inventing successful eligibility.
// sshExecutionAdmissionTx предоставляет лишь nested admission и rollback; неожиданный owner query либо commit приводит к отказу вместо вымышленной successful eligibility.
type sshExecutionAdmissionTx struct {
	pgx.Tx
	child       *sshExecutionAdmissionTx
	beginErr    error
	rollbackErr error
	rolledBack  bool
}

// Begin returns the owned savepoint or an injected infrastructure failure without changing the original owner transaction.
// Begin возвращает owned savepoint либо injected infrastructure failure без изменения original owner transaction.
func (tx *sshExecutionAdmissionTx) Begin(context.Context) (pgx.Tx, error) {
	if tx.beginErr != nil {
		return nil, tx.beginErr
	}
	return tx.child, nil
}

// Rollback requires an independent live bounded cleanup context even when the caller has already cancelled admission.
// Rollback требует independent live bounded cleanup context, даже когда caller уже отменил admission.
func (tx *sshExecutionAdmissionTx) Rollback(ctx context.Context) error {
	if ctx.Err() != nil {
		return errors.New("admission rollback inherited cancellation")
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return errors.New("admission rollback is unbounded")
	}
	tx.rolledBack = true
	return tx.rollbackErr
}

// TestSSHExecutionClaimSavepointPreservesInfrastructure distinguishes a safely rolled-back known denial from failed cleanup; a cancelled caller cannot leave partial candidate state eligible for settlement.
// TestSSHExecutionClaimSavepointPreservesInfrastructure отличает safely rolled-back known denial от failed cleanup; cancelled caller не оставляет partial candidate state, пригодный для settlement.
func TestSSHExecutionClaimSavepointPreservesInfrastructure(t *testing.T) {
	for _, item := range []struct {
		name        string
		beginErr    error
		rollbackErr error
		want        error
	}{
		{name: "known denial", want: errs.ErrForbidden},
		{name: "already rolled back", rollbackErr: pgx.ErrTxClosed, want: errs.ErrForbidden},
		{name: "failed rollback", rollbackErr: errors.New("synthetic rollback failure"), want: errs.ErrUnavailable},
		{name: "failed savepoint", beginErr: errors.New("synthetic savepoint failure"), want: errs.ErrUnavailable},
	} {
		t.Run(item.name, func(t *testing.T) {
			child := &sshExecutionAdmissionTx{rollbackErr: item.rollbackErr}
			parent := &sshExecutionAdmissionTx{child: child, beginErr: item.beginErr}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			claim, err := (&Repository{}).claimSSHInvocationCandidateTx(ctx, parent, scope{}, value.Principal{}, "gateway_fixture", integrationInvocationCandidate{})
			if claim != nil || !errors.Is(err, item.want) || child.rolledBack != (item.beginErr == nil) {
				t.Fatal("candidate failure retained partial admission or concealed infrastructure")
			}
			if parent.rolledBack {
				t.Fatal("candidate helper rolled back the caller-owned outer transaction")
			}
		})
	}
}
