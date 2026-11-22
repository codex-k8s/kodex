package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCaptureReject records a proven content refusal without changing native truth.
// queryKubernetesCaptureReject фиксирует доказанный content refusal, не меняя native truth.
//
//go:embed sql/kubernetes_capture__reject.sql
var queryKubernetesCaptureReject string

// rejectKubernetesCaptureContent closes only the original live attempt after a known inspection
// rejection. It leaves accepted source bytes and any one-PUT intent under canonical retention.
// rejectKubernetesCaptureContent закрывает только original live attempt после known inspection
// rejection. Accepted source bytes и one-PUT intent остаются под canonical retention.
func (repository *Repository) rejectKubernetesCaptureContent(ctx context.Context, p value.Principal, binding value.ManagedKubernetesCaptureTaskBinding, record kubernetesCaptureTaskRecord) (result entity.ManagedKubernetesCaptureReceipt, resultErr error) {
	if repository.kubernetesCaptureCaller(ctx, p, binding, "platform.artifacts.kubernetes-capture.finalize") != nil {
		return result, errs.ErrForbidden
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if _, err := repository.lockKubernetesCaptureOwnerTx(ctx, tx, record, binding); err != nil {
		return result, err
	}
	args, err := kubernetesCaptureTaskArgs(binding)
	if err != nil {
		return result, err
	}
	tag, err := tx.Exec(ctx, queryKubernetesCaptureReject, args)
	if err != nil || tag.RowsAffected() != 1 {
		return result, errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	updated, err := readKubernetesCaptureTask(ctx, repository.pool, binding)
	if err != nil {
		return result, err
	}
	return updated.receipt, nil
}
