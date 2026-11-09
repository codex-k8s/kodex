package platform

import (
	"context"
	_ "embed"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

// queryIntegrationApprovalSelfCatalog enforces the same initiator and current visibility for list and mutation preauthorization; no other user's scope values are projected.
// queryIntegrationApprovalSelfCatalog проверяет одинаковые initiator и current visibility для list и mutation preauthorization; чужие scope values не проецируются.
//
//go:embed sql/integration_approval__self_catalog.sql
var queryIntegrationApprovalSelfCatalog string

// queryIntegrationApprovalSelfLock serializes explicit revoke with the owner call-admission fence on the exact consent row.
// queryIntegrationApprovalSelfLock упорядочивает explicit revoke и owner call-admission fence на exact consent row.
//
//go:embed sql/integration_approval__self_lock.sql
var queryIntegrationApprovalSelfLock string

// queryIntegrationApprovalSelfRevoke applies OCC once; the database assigns monotonic version and the revocation timestamp.
// queryIntegrationApprovalSelfRevoke применяет OCC однократно; БД назначает monotonic version и время отзыва.
//
//go:embed sql/integration_approval__self_revoke.sql
var queryIntegrationApprovalSelfRevoke string

// scanIntegrationApprovalConsent reads only the bounded self projection and leaves expiry calculation to PostgreSQL's authoritative clock.
// scanIntegrationApprovalConsent читает лишь bounded self projection; expiry вычисляется авторитетными часами PostgreSQL.
func scanIntegrationApprovalConsent(row pgx.Row) (entity.IntegrationApprovalConsent, error) {
	var item entity.IntegrationApprovalConsent
	err := row.Scan(&item.Ref, &item.Version, &item.ConnectionRef, &item.ConnectionName, &item.CapabilityKey, &item.AgentRef, &item.RootRunRef, &item.Lifetime, &item.State, &item.ScopePaths, &item.ScopeValuesJSON, &item.MaxEffects, &item.ReservedEffects, &item.CreatedAt, &item.ExpiresAt, &item.RevokedAt)
	return item, err
}

// integrationApprovalSelfArgs never accepts a subject selector from the request; current owner scope supplies both tenant and actor.
// integrationApprovalSelfArgs не принимает subject selector из request; current owner scope задаёт tenant и actor.
func integrationApprovalSelfArgs(current scope, ref, after string, limit int32) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{"organization_id": current.organizationID, "subject_id": current.actorID, "ref": ref, "after": after, "limit": limit}
}

// ListIntegrationApprovalConsents reads an actor-bound paginated snapshot; ordinary administrators cannot list another initiator's consent by supplying a run or subject ID.
// ListIntegrationApprovalConsents читает actor-bound paginated snapshot; обычный администратор не получает чужие consents через run либо subject ID.
func (repository *Repository) ListIntegrationApprovalConsents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.IntegrationApprovalConsent, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if p.CallerWorkload != "control-api-gateway" || p.Permission != "platform.integration-approval-consents.list" {
		return nil, "", errs.ErrForbidden
	}
	if filter.Query != "" || filter.State != "" || filter.ResourceRef != "" || filter.ProjectRef != "" || filter.Page.Size < 0 || filter.Page.Size > 100 {
		return nil, "", errs.ErrInvalid
	}
	if filter.Page.Size == 0 {
		filter.Page.Size = 50
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return nil, "", err
	}
	if current.authorityProjectID != "" {
		return nil, "", errs.ErrForbidden
	}
	const kind = "INTEGRATION_APPROVAL_CONSENT"
	after, err := decodeCatalogCursor(current, kind, filter)
	if err != nil {
		return nil, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryIntegrationApprovalSelfCatalog, integrationApprovalSelfArgs(current, "", after, filter.Page.Size+1))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	items := []entity.IntegrationApprovalConsent{}
	for rows.Next() {
		item, err := scanIntegrationApprovalConsent(rows)
		if err != nil {
			rows.Close()
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	items, next := catalogPageWithCursor(current, kind, filter, items, filter.Page.Size, func(item entity.IntegrationApprovalConsent) string { return item.Ref })
	if err := tx.Commit(ctx); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}

// authorizeIntegrationApprovalRevokeTx rechecks self ownership and current run/connection visibility before receipt replay; a guessed consent ref reveals no protected scope fields.
// authorizeIntegrationApprovalRevokeTx повторно проверяет self ownership и current run/connection visibility до receipt replay; guessed ref не раскрывает защищённые scope fields.
func (repository *Repository) authorizeIntegrationApprovalRevokeTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, ok := input.Payload.(command.IntegrationApprovalConsentInput)
	if !ok || len(payload.ConsentRef) != 36 || !strings.HasPrefix(payload.ConsentRef, "ias_") {
		return errs.ErrInvalid
	}
	for _, c := range payload.ConsentRef[4:] {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return errs.ErrInvalid
		}
	}
	if current.authorityProjectID != "" || input.Principal.CallerWorkload != "control-api-gateway" || input.Principal.Permission != "platform.integration-approval-consents.revoke" {
		return errs.ErrForbidden
	}
	if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion < 1 {
		return errs.ErrInvalid
	}
	var id string
	if err := tx.QueryRow(ctx, queryIntegrationApprovalSelfLock, current.organizationID, current.actorID, payload.ConsentRef).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	} else if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	_, err := scanIntegrationApprovalConsent(tx.QueryRow(ctx, queryIntegrationApprovalSelfCatalog, integrationApprovalSelfArgs(current, payload.ConsentRef, "", 1)))
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// revokeIntegrationApprovalConsentTx closes future call admissions in the existing owner receipt/audit/event transaction, while preserving reserved counters and started effects.
// revokeIntegrationApprovalConsentTx закрывает будущие call admissions в существующей owner receipt/audit/event transaction, сохраняя reserved counters и начатые effects.
func (repository *Repository) revokeIntegrationApprovalConsentTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload := input.Payload.(command.IntegrationApprovalConsentInput)
	before, err := scanIntegrationApprovalConsent(tx.QueryRow(ctx, queryIntegrationApprovalSelfCatalog, integrationApprovalSelfArgs(current, payload.ConsentRef, "", 1)))
	if err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if before.Version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if before.RevokedAt == nil {
		tag, err := tx.Exec(ctx, queryIntegrationApprovalSelfRevoke, current.organizationID, payload.ConsentRef, before.Version)
		if err != nil {
			return commandOutcome{}, serializableTransactionError(err, errs.ErrUnavailable)
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
	}
	after, err := scanIntegrationApprovalConsent(tx.QueryRow(ctx, queryIntegrationApprovalSelfCatalog, integrationApprovalSelfArgs(current, payload.ConsentRef, "", 1)))
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{IntegrationApprovalConsent: &after}, resourceKind: "INTEGRATION_APPROVAL_CONSENT", resourceRef: after.Ref, summary: "i18n:INTEGRATION_APPROVAL_REVOKED"}, nil
}
