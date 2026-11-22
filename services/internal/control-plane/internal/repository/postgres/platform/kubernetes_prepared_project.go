package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesPreparedProjectLock serializes a purpose-bound object writer with project
// trash and automatic purge without changing the project or granting publication authority.
// queryKubernetesPreparedProjectLock сериализует purpose-bound writer объекта с trash и
// автоматическим purge проекта, не изменяя проект и не выдавая право публикации.
//
//go:embed sql/kubernetes_prepared__project_lock.sql
var queryKubernetesPreparedProjectLock string

// lockKubernetesPreparedProjectTx holds the exact active project share lock before any prepared
// row lock and through PUT plus its durable outcome. Projectless captures keep their existing
// boundary; other prepared profiles retain their own lifecycle. A closed project refuses new PUT,
// while the original durable intent remains available for reconciliation and cleanup.
// lockKubernetesPreparedProjectTx удерживает share lock точного активного проекта до блокировки
// prepared row и до завершения PUT с устойчивой записью результата. Capture без проекта сохраняет
// прежнюю границу; остальные prepared profiles используют свой lifecycle. Закрытый проект
// запрещает новый PUT, сохраняя исходный durable intent для reconciliation и cleanup.
func lockKubernetesPreparedProjectTx(ctx context.Context, tx pgx.Tx, binding preparedContentBinding) error {
	if binding.SourceProfile != "KUBERNETES_OUTPUT" && binding.SourceProfile != "KUBERNETES_FILE_SOURCE" || binding.ProjectID == "" {
		return nil
	}
	var id string
	err := tx.QueryRow(ctx, queryKubernetesPreparedProjectLock, pgx.StrictNamedArgs{
		"project_id": binding.ProjectID, "organization_id": binding.OrganizationID,
	}).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if id != binding.ProjectID {
		return errs.ErrConflict
	}
	return nil
}
