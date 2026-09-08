package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/email_mailbox_observation_advance.sql
var queryEmailMailboxObservationAdvance string

// Только эти typed transitions меняют observation/grant lifecycle, но не
// mailbox specification/credentials. Они берут publication lock до row locks.
func preservesMailboxSpecification(kind command.Kind) bool {
	return kind == command.TestConnection || kind == command.CompleteConnectionTest || kind == command.ChangeIntegrationGrant
}

// Меняется лишь delivery OCC precondition. Ref/revision/document/digest и
// credential generation неизменны; прежний drift никогда не усыновляется.
func advanceMailboxObservation(ctx context.Context, tx pgx.Tx, organizationID, connectionID string, before, after int64) error {
	if before < 1 || after != before+1 {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryEmailMailboxObservationAdvance, pgx.StrictNamedArgs{
		"organization_id": organizationID, "connection_id": connectionID, "previous_version": before, "current_version": after,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
