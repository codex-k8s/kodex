package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// auditMailboxOwner inserts an owner audit row with current actor/correlation, anchoring to the
// connection or organization when connection ref is empty. Ref/Exec failures are Unavailable; no
// transaction commit, event or delivery occurs here.
// auditMailboxOwner вставляет owner audit с текущими actor/correlation, привязывая запись к
// connection либо организации при пустом connection ref. Ошибки ref/Exec дают Unavailable; Commit,
// event и доставка здесь не выполняются.
func auditMailboxOwner(ctx context.Context, tx pgx.Tx, current scope, connectionRef, action, summary string) error {
	ref, err := newRef("aud")
	if err != nil {
		return errs.ErrUnavailable
	}
	kind, resourceRef := "INTEGRATION", connectionRef
	if resourceRef == "" {
		kind, resourceRef = "ORGANIZATION", current.organizationRef
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, ref, current.organizationID, nil, current.actorID, action, kind, resourceRef, summary, current.correlationRef); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
