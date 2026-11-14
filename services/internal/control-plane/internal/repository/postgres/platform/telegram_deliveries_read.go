package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryTelegramListDeliveries filters project, connection and Run eligibility before pagination and never returns a private destination or message body.
// queryTelegramListDeliveries проверяет доступ к проекту, подключению и Run до pagination и никогда не возвращает private destination либо message body.
//
//go:embed sql/telegram_list_deliveries.sql
var queryTelegramListDeliveries string

// ListInteractionDeliveries reads an authorized bounded snapshot using the same domain Run visibility as direct Run queries; cursors remain tenant, actor and project scoped.
// ListInteractionDeliveries читает разрешённый ограниченный snapshot с тем же domain Run visibility, что прямые Run queries; cursors ограничены tenant, actor и project.
func (repository *Repository) ListInteractionDeliveries(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.InteractionDeliveryView, string, error) {
	if p.CallerWorkload != "control-api-gateway" || p.Permission != "platform.query.interaction-deliveries.list" {
		return nil, "", errs.ErrForbidden
	}
	if filter.Page.Size == 0 {
		filter.Page.Size = 50
	}
	if filter.Page.Size < 1 || filter.Page.Size > 100 || filter.Query != "" || filter.State != "" || filter.ResourceRef != "" {
		return nil, "", errs.ErrInvalid
	}
	current, tx, err := repository.beginScopedTransaction(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	projectID, err := telegramProjectOwnerTx(ctx, tx, current, filter.ProjectRef, "project.view")
	if err != nil {
		return nil, "", err
	}
	const kind = "INTERACTION_DELIVERY"
	after, err := decodeCatalogCursor(current, kind, filter)
	if err != nil {
		return nil, "", err
	}
	rows, err := tx.Query(ctx, queryTelegramListDeliveries, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"actor_id": current.actorID, "project_id": projectID, "after": after, "limit": filter.Page.Size + 1})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := []entity.InteractionDeliveryView{}
	for rows.Next() {
		var item entity.InteractionDeliveryView
		if err := rows.Scan(&item.Ref, &item.Version, &item.ConnectionRef, &item.RunRef, &item.GateRef, &item.EventType, &item.State,
			&item.Attempt, &item.Retryable, &item.SafeErrorCode, &item.CreatedAt, &item.AvailableAt, &item.CompletedAt, &item.ReceiptRef); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	rows.Close()
	items, next := catalogPageWithCursor(current, kind, filter, items, filter.Page.Size, func(item entity.InteractionDeliveryView) string { return item.Ref })
	if err := tx.Commit(ctx); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}
