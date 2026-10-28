package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// callbackJoinMode selects only existing native delivery or the owner-proved legacy rework boundary, never arbitrary SQL supplied by a caller.
// callbackJoinMode выбирает только обычную native доставку либо доказанную owner rework границу, а не SQL вызывающего.
type callbackJoinMode uint8

const (
	// callbackJoinNative retains the ordinary callback eligibility unchanged.
	// callbackJoinNative сохраняет обычный допуск callback без изменений.
	callbackJoinNative callbackJoinMode = iota
	// callbackJoinLegacyRework admits a sealed coordinator only after its exact newly accepted owner-attempt receipts.
	// callbackJoinLegacyRework допускает sealed coordinator лишь после точных принятых receipts новой owner attempt.
	callbackJoinLegacyRework
)

// queryLegacyReworkContinuation projects current accepted child attempts from their stored gates and immutable callback receipts.
// queryLegacyReworkContinuation проецирует текущие принятые child attempts по сохранённым Gates и immutable receipts.
//
//go:embed sql/legacy_workflow_rework__continuation.sql
var queryLegacyReworkContinuation string

// queryLegacyReworkNode reads only the private assignment discriminator, never an authority flag from a model.
// queryLegacyReworkNode читает лишь private discriminator назначения, а не authority flag модели.
//
//go:embed sql/legacy_workflow_rework__node.sql
var queryLegacyReworkNode string

// queryLegacyReworkCallbackOrigin resolves the immutable original edge from the completed final execution tuple.
// queryLegacyReworkCallbackOrigin разрешает immutable исходное ребро по completed final execution tuple.
//
//go:embed sql/legacy_workflow_rework__callback_origin.sql
var queryLegacyReworkCallbackOrigin string

// queryLegacyReworkCallbackReceipt retains original callback identity and pins artifacts to the proved final RuntimeRevision.
// queryLegacyReworkCallbackReceipt сохраняет исходную callback identity и связывает материалы с доказанной final RuntimeRevision.
//
//go:embed sql/legacy_workflow_rework__callback_receipt.sql
var queryLegacyReworkCallbackReceipt string

// queryLegacyReworkDeliveryPending reads the owner-derived delivery barrier without inventing an active model or receipt.
// queryLegacyReworkDeliveryPending читает owner-derived delivery barrier без выдуманной активной модели либо receipt.
//
//go:embed sql/legacy_workflow_rework__delivery_pending.sql
var queryLegacyReworkDeliveryPending string

// legacyReworkDeliveryPendingTx preserves SQL failure as an error, never as an absent delivery obligation.
// legacyReworkDeliveryPendingTx сохраняет ошибку SQL, не трактуя её как отсутствие обязательной доставки.
func legacyReworkDeliveryPendingTx(ctx context.Context, tx pgx.Tx, current scope, rootID string) (bool, error) {
	var pending bool
	if tx.QueryRow(ctx, queryLegacyReworkDeliveryPending, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": rootID}).Scan(&pending) != nil {
		return false, errs.ErrUnavailable
	}
	return pending, nil
}

// isLegacyReworkNodeTx checks the server-owned assignment in the current owner transaction; database errors never select the ordinary branch.
// isLegacyReworkNodeTx проверяет server-owned assignment в текущей транзакции; ошибка БД не выбирает обычную ветку.
func isLegacyReworkNodeTx(ctx context.Context, tx pgx.Tx, current scope, nodeID string) (bool, error) {
	var gateID string
	if tx.QueryRow(ctx, queryLegacyReworkNode, pgx.StrictNamedArgs{"organization_id": current.organizationID, "node_id": nodeID}).Scan(&gateID) != nil {
		return false, errs.ErrUnavailable
	}
	return gateID != "", nil
}

// scheduleLegacyReworkContinuationTx reuses native turn/edge materialization after the narrower owner-rework selector has proved its sealed boundary.
// scheduleLegacyReworkContinuationTx повторно использует native materialization turn/edge после доказательства узкой owner-rework границы.
func (repository *Repository) scheduleLegacyReworkContinuationTx(ctx context.Context, tx pgx.Tx, current scope, parentID, projectID string) (bool, error) {
	return repository.scheduleCallbackContinuationTaskForMode(ctx, tx, current, parentID, projectID, callbackJoinLegacyRework)
}
