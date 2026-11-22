package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryEmailInvocationGate identifies an email invocation only from the locked gate's existing
// owner-resolved identifier; other integrations keep their existing approval paths.
// queryEmailInvocationGate распознаёт email invocation только по существующему identifier,
// разрешённому владельцем заблокированного Gate; остальные интеграции сохраняют прежние пути.
//
//go:embed sql/email_invocation__gate.sql
var queryEmailInvocationGate string

// requireEmailGateMailboxTx checks the root initiator's pinned mailbox before either EACH or
// SCOPED approval. The approver cannot become the credential owner or refresh an obsolete pin.
// requireEmailGateMailboxTx проверяет закреплённый mailbox корневого инициатора до EACH либо
// SCOPED approval. Approver не становится credential owner и не обновляет устаревший pin.
func (repository *Repository) requireEmailGateMailboxTx(ctx context.Context, tx pgx.Tx, current scope, invocationID string) error {
	var ref string
	err := tx.QueryRow(ctx, queryEmailInvocationGate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": invocationID}).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	_, err = repository.requireEmailInvocationMailboxTx(ctx, tx, current, ref)
	return err
}
