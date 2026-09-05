package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

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
