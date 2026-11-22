package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/jackc/pgx/v5"
)

var (
	// queryEmailApprovalScopePin stores only the fresh mailbox context of an existing owner consent; it creates no independent consent or effect reservation.
	// queryEmailApprovalScopePin сохраняет только текущий mailbox context существующего owner consent; отдельного согласия либо effect reservation не создаёт.
	//go:embed sql/email_scope__pin.sql
	queryEmailApprovalScopePin string
	// queryEmailApprovalScopeMatches compares a consent's immutable mailbox context with freshly resolved current state.
	// queryEmailApprovalScopeMatches сверяет неизменный mailbox context согласия с заново разрешённым текущим состоянием.
	//go:embed sql/email_scope__matches.sql
	queryEmailApprovalScopeMatches string
	// queryEmailApprovalScopeInvocation resolves the scope UUID from the stored invocation rather than accepting a scope selector from the gateway.
	// queryEmailApprovalScopeInvocation разрешает scope UUID из сохранённой invocation вместо scope selector gateway.
	//go:embed sql/email_scope__invocation.sql
	queryEmailApprovalScopeInvocation string
)

// pinEmailApprovalScopeTx binds the existing scoped approval to its exact initial mailbox pin, refusing approval after owner, credential or configuration changes.
// pinEmailApprovalScopeTx связывает существующее scoped approval с точным первоначальным mailbox pin, отклоняя подтверждение после смены владельца, credentials либо конфигурации.
func pinEmailApprovalScopeTx(ctx context.Context, tx pgx.Tx, current scope, invocationID, approvalScopeID string) error {
	tag, err := tx.Exec(ctx, queryEmailApprovalScopePin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": invocationID, "approval_scope_id": approvalScopeID})
	if err != nil {
		return serializableTransactionError(err, mapWriteError(err))
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrForbidden
	}
	return nil
}

// emailApprovalScopeMatchesTx never upgrades a stale context pin into current consent; the caller must create a new ordinary owner gate when it returns false.
// emailApprovalScopeMatchesTx не превращает устаревший context pin в текущее согласие; при false вызывающий создаёт новый обычный owner gate.
func emailApprovalScopeMatchesTx(ctx context.Context, tx pgx.Tx, current scope, approvalScopeID string, owner emailCredentialOwnerRow, mailbox emailpolicy.MailboxProjection) (bool, error) {
	if approvalScopeID == "" {
		return false, nil
	}
	var matches bool
	err := tx.QueryRow(ctx, queryEmailApprovalScopeMatches, pgx.StrictNamedArgs{"organization_id": current.organizationID, "approval_scope_id": approvalScopeID,
		"connection_id": owner.connectionID, "owner_subject_id": owner.owner.SubjectID, "owner_generation": owner.owner.Generation,
		"mailbox_ref": mailbox.Ref, "mailbox_revision": mailbox.Revision, "mailbox_source_digest": mailbox.SourceDigest}).Scan(&matches)
	if err != nil {
		return false, serializableTransactionError(err, errs.ErrUnavailable)
	}
	return matches, nil
}

// requireEmailApprovalScopeTx checks the stored consent context after the common canonical claim predicate and fresh scope lock; metadata alone never bypasses expiry, revoke or effect budgets.
// requireEmailApprovalScopeTx проверяет сохранённый context согласия после общей canonical claim predicate и текущей scope lock; metadata не обходят expiry, revoke и effect budgets.
func requireEmailApprovalScopeTx(ctx context.Context, tx pgx.Tx, current scope, choice emailInvocationOwnership) error {
	var approvalScopeID string
	if tx.QueryRow(ctx, queryEmailApprovalScopeInvocation, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": choice.invocationID}).Scan(&approvalScopeID) != nil {
		return errs.ErrForbidden
	}
	matches, err := emailApprovalScopeMatchesTx(ctx, tx, current, approvalScopeID, choice.owner, choice.mailbox)
	if err != nil {
		return err
	}
	if !matches {
		return errs.ErrForbidden
	}
	return nil
}
