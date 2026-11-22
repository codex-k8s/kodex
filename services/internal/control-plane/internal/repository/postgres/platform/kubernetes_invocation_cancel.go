package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesInvocationCancelLock locks only the exact durable original claim, never a replacement.
// queryKubernetesInvocationCancelLock блокирует лишь exact durable original claim, не replacement.
//
//go:embed sql/kubernetes_invocation__cancel_lock.sql
var queryKubernetesInvocationCancelLock string

// queryKubernetesInvocationCancel derives uncertainty from stored attempts and invokes existing graph closure.
// queryKubernetesInvocationCancel выводит uncertainty из stored attempts и вызывает existing graph closure.
//
//go:embed sql/kubernetes_invocation__cancel.sql
var queryKubernetesInvocationCancel string

// CancelManagedKubernetesInvocation atomically revokes only the original execution's live credit.
// It needs no current target/credential/grant because it grants no new work or output. Historical receipts
// remain immutable; unsettled attempts stay UNKNOWN, and positive remote/process join is never fabricated.
// No event is emitted; exact original-binding Get is the authoritative metadata readback.
// CancelManagedKubernetesInvocation атомарно отзывает лишь live credit original execution.
// Не требует current target/credential/grant, поскольку не выдаёт work/output. Historical receipts
// immutable; unsettled attempts остаются UNKNOWN, positive remote/process join не выдумывается.
// События нет; exact original-binding Get является authoritative metadata readback.
func (repository *Repository) CancelManagedKubernetesInvocation(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding) (result entity.ManagedKubernetesInvocationReceipt, resultErr error) {
	if err := kubernetesInvocationGatewayCaller(p, binding, "platform.runtime.kubernetes.cancel"); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	args, err := kubernetesInvocationCompletionArgs(current.organizationID, binding)
	if err != nil {
		return result, err
	}
	// Lock arguments intentionally contain no unverified plan or caller-chosen state.
	// Lock arguments не содержат unverified plan либо caller-chosen state.
	lockArgs := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": binding.InvocationRef, "input_digest": binding.InputDigest, "generation": binding.Generation, "lease_ref": binding.LeaseRef, "fence_digest": kubernetesHealthFenceDigest(binding.Fence), "instance": binding.WorkloadInstance}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	var id string
	err = tx.QueryRow(ctx, queryKubernetesInvocationCancelLock, lockArgs).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	record, err := readKubernetesInvocationCompletion(tx.QueryRow(ctx, queryKubernetesInvocationCompletionRead, args), binding)
	if err != nil {
		return result, err
	}
	if record.receipt.State != enum.KubernetesInvocationRunning {
		return record.receipt, nil
	}
	tag, err := tx.Exec(ctx, queryKubernetesInvocationCancel, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": id})
	if err != nil {
		return result, mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return result, errs.ErrConflict
	}
	record, err = readKubernetesInvocationCompletion(tx.QueryRow(ctx, queryKubernetesInvocationCompletionRead, args), binding)
	if err != nil {
		return result, err
	}
	if record.receipt.State == enum.KubernetesInvocationRunning || record.receipt.CompletionRecorded {
		return result, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	return record.receipt, nil
}
