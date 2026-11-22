package platform

import (
	"context"
	_ "embed"

	"errors"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"strings"
)

// queryKubernetesProjectRoots resolves all original project roots, including completed roots
// retaining native invocations; it carries no payload or authority and does not lock prepared rows.
// queryKubernetesProjectRoots разрешает все исходные roots проекта, включая завершённые roots
// с оставшимися native invocations; не несёт payload/authority и не блокирует prepared rows.
//
//go:embed sql/kubernetes_project__roots.sql
var queryKubernetesProjectRoots string

// queryKubernetesProjectUnreconciled reads the post-lock owner count of object intents whose
// immutable provider version is still unknown; it never treats an absent observation as deletion.
// queryKubernetesProjectUnreconciled читает после lock owner count object intents с ещё неизвестной
// immutable provider version; наблюдение отсутствия не подменяет подтверждённое удаление.
//
//go:embed sql/kubernetes_project__unreconciled.sql
var queryKubernetesProjectUnreconciled string

// requireKubernetesProjectReconciledTx retains ACTIVE project eligibility when a staged object
// still needs original-root reconciliation. The caller already holds the project lock; root locks
// serialize frame reservations, then a fresh READ COMMITTED statement observes completed writers.
// No cancellation, event, grant revocation or object operation happens before this guard succeeds.
// requireKubernetesProjectReconciledTx сохраняет ACTIVE eligibility проекта, когда staged object
// ещё требует original-root reconciliation. Caller уже держит project lock; root locks сериализуют
// frame reservations, свежий READ COMMITTED statement видит завершившихся writers. До успешной
// проверки не происходят cancel, event, отзыв grant либо object operation.
func requireKubernetesProjectReconciledTx(ctx context.Context, tx pgx.Tx, current scope, projectID string) error {
	rows, err := tx.Query(ctx, queryKubernetesProjectRoots, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID})
	if err != nil {
		return errs.ErrUnavailable
	}
	var roots []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		roots = append(roots, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	for _, id := range roots {
		if _, err := lockOwnerGateRootTx(ctx, tx, id); err != nil {
			return err
		}
	}
	var manualCount int64
	if err := tx.QueryRow(ctx, queryArtifactManualProjectPending, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID}).Scan(&manualCount); err != nil || manualCount < 0 {
		return errs.ErrUnavailable
	}
	if manualCount > 0 {
		return errs.ErrArtifactPurgeReconciliationRequired
	}
	var count int64
	if err := tx.QueryRow(ctx, queryKubernetesProjectUnreconciled, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID}).Scan(&count); err != nil {
		return errs.ErrUnavailable
	}
	if count < 0 {
		return errs.ErrUnavailable
	}
	if count > 0 {
		return errs.ErrKubernetesStagingReconciliationRequired
	}
	return nil
}

// queryKubernetesProjectObjectReceipt resolves one immutable receipt through the original project
// lineage; conflicting receipts for one key/version are deliberately not collapsed.
// queryKubernetesProjectObjectReceipt разрешает один immutable receipt через original project
// lineage; конфликтующие receipts одного key/version намеренно не объединяются.
//
//go:embed sql/kubernetes_project__object_receipt.sql
var queryKubernetesProjectObjectReceipt string

// ProjectPurgeKubernetesObjectReceipt proves the exact pending-project object tuple for the
// existing purge consumer. A path prefix cannot establish ownership; missing/ambiguous source
// denies deletion, and no payload, new object write or native execution occurs here.
// ProjectPurgeKubernetesObjectReceipt доказывает exact pending-project object tuple существующему
// purge consumer. Префикс пути не доказывает владение; missing/ambiguous source запрещает удаление.
// Payload, новая запись объекта либо native execution здесь не выполняются.
func (repository *Repository) ProjectPurgeKubernetesObjectReceipt(ctx context.Context, candidate ProjectPurgeCandidate, item ProjectPurgeExternalItem) (objectstorage.Receipt, error) {
	var receipt objectstorage.Receipt
	if item.Kind != "OBJECT" || item.Version == "" {
		return receipt, errs.ErrNotFound
	}
	err := repository.pool.QueryRow(ctx, queryKubernetesProjectObjectReceipt, pgx.StrictNamedArgs{
		"organization_id": candidate.OrganizationID, "project_id": candidate.ProjectID, "project_ref": candidate.ProjectRef,
		"object_key": item.Target, "object_version": item.Version,
	}).Scan(&receipt.Key, &receipt.VersionID, &receipt.ETag, &receipt.Digest, &receipt.SizeBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstorage.Receipt{}, errs.ErrNotFound
	}
	if err != nil {
		return objectstorage.Receipt{}, errs.ErrUnavailable
	}
	if receipt.Key != item.Target || receipt.VersionID != item.Version || receipt.ETag == "" || !strings.HasPrefix(receipt.Digest, "sha256:") || !exactSHA256(strings.TrimPrefix(receipt.Digest, "sha256:")) || receipt.SizeBytes < 0 {
		return objectstorage.Receipt{}, errs.ErrUnavailable
	}
	return receipt, nil
}

// queryKubernetesProjectCloseCaptures fences publication attempts under stream/capture locks while
// retaining immutable source and completed history. Restoring the project does not resume a task.
// queryKubernetesProjectCloseCaptures закрывает publication attempts под stream/capture locks,
// сохраняя immutable source и завершённую историю. Restore проекта не возобновляет task.
//
//go:embed sql/kubernetes_project__close_captures.sql
var queryKubernetesProjectCloseCaptures string

// closeKubernetesProjectCapturesTx uses the existing project transaction after execution closure.
// Unpublished captures remain WAITING_OWNER so an authorized original actor can explicitly repair
// after project restoration; neither native effects nor attempts/counters are reset. The enclosing
// PROJECT_CHANGED event and authoritative capture/project reads describe the atomic closure.
// closeKubernetesProjectCapturesTx использует существующую project transaction после execution
// closure. Непубликовавшиеся captures остаются WAITING_OWNER для явного repair original actor после
// restore проекта; native effects и attempt counters не возобновляются/не обнуляются. Внешний
// PROJECT_CHANGED event и авторитетные capture/project reads описывают атомарное закрытие.
func closeKubernetesProjectCapturesTx(ctx context.Context, tx pgx.Tx, current scope, projectID string) error {
	if _, err := tx.Exec(ctx, queryKubernetesProjectCloseCaptures, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
