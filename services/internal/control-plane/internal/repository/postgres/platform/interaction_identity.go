package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryInteractionIdentityInsert embeds an organization mapping to an active USER with active
// organization-level membership, storing exact connection version and external team/channel/user
// digest. It relies on caller connection authorization/locking and does not accept arbitrary
// payload identity as authority.
// queryInteractionIdentityInsert встраивает organization mapping к active USER с active
// organization-level membership, сохраняя exact connection version и external team/channel/user
// digest. Полагается на caller connection authorization/locking и не принимает произвольную
// payload identity как authority.
//
//go:embed sql/interaction_identity_insert.sql
var queryInteractionIdentityInsert string

// queryInteractionIdentityGet embeds one tenant/ref identity projection with connection and
// subject refs. It has no actor permission or current connection-version/state predicate; those
// checks belong to mutation/read consumers.
// queryInteractionIdentityGet встраивает одну tenant/ref identity projection с connection и
// subject refs. Actor permission и current connection-version/state predicate отсутствуют; их
// проверяют mutation/read consumers.
//
//go:embed sql/interaction_identity_get.sql
var queryInteractionIdentityGet string

// queryInteractionIdentityRevoke embeds exact tenant/ref/version ACTIVE-to-REVOKED transition with
// revoking actor and timestamp. No row represents version/state mismatch; caller access and
// transaction ownership remain separate.
// queryInteractionIdentityRevoke встраивает exact tenant/ref/version ACTIVE-to-REVOKED transition
// с revoking actor и timestamp. No row означает version/state mismatch; caller access и
// transaction ownership остаются отдельными.
//
//go:embed sql/interaction_identity_revoke.sql
var queryInteractionIdentityRevoke string

// queryInteractionIdentityList embeds ordered tenant identity pagination with optional connection
// filter and integration.manage visibility before limit. Organization access.manage and cursor
// validation still belong to the reader.
// queryInteractionIdentityList встраивает ordered tenant identity pagination с optional connection
// filter и integration.manage visibility до limit. Organization access.manage и cursor validation
// остаются у reader.
//
//go:embed sql/interaction_identity_list.sql
var queryInteractionIdentityList string

// queryInteractionIdentityResolve embeds exact active Mattermost
// connection-version/team/channel/user-digest mapping to an active USER with organization
// membership, under SHARE locks. It returns the mapped human identity/role; a caller-supplied user
// locator alone is not sufficient.
// queryInteractionIdentityResolve встраивает exact active Mattermost
// connection-version/team/channel/user-digest mapping к active USER с organization membership под
// SHARE locks. Возвращает mapped human identity/role; одного caller-supplied user locator
// недостаточно.
//
//go:embed sql/interaction_identity_resolve.sql
var queryInteractionIdentityResolve string

// scanInteractionIdentity scans the fixed nine-field mapping projection, mapping no row to
// NotFound and other scan errors to Unavailable. Partial result fields can accompany an error; it
// validates neither authority nor current connection freshness.
// scanInteractionIdentity читает fixed nine-field mapping projection, отображая no row в NotFound,
// прочие scan errors — в Unavailable. Partial result fields могут сопровождать error; authority и
// current connection freshness не проверяет.
func scanInteractionIdentity(row rowScanner) (entity.InteractionIdentity, error) {
	var result entity.InteractionIdentity
	err := row.Scan(&result.Ref, &result.Version, &result.ConnectionRef, &result.ConnectionVersion, &result.ExternalTeamRef,
		&result.ExternalChannelRef, &result.ExternalUserDigest, &result.SubjectRef, &result.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

// changeInteractionIdentity accepts typed unscoped mutation with expected version; bind validates
// external locators/digest, integration.manage and active Mattermost connection OCC before
// inserting the member mapping. The other branch reauthorizes the resolved connection and revokes
// an exact active mapping version. It returns command outcome only; outer dispatch,
// receipt/audit/event handling and atomic commit remain with the command owner.
// changeInteractionIdentity принимает typed unscoped mutation с expected version; bind проверяет
// external locators/digest, integration.manage и active Mattermost connection OCC до вставки
// member mapping. Другая ветка заново авторизует resolved connection и отзывает exact active
// mapping version. Возвращает только command outcome; outer dispatch, receipt/audit/event handling
// и атомарный commit остаются у command owner.
func (repository *Repository) changeInteractionIdentity(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.InteractionIdentityInput)
	if !ok || input.Mutation.ExpectedVersion == nil || current.authorityProjectID != "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var result entity.InteractionIdentity
	if input.Kind == command.BindInteractionIdentity {
		for _, ref := range []string{payload.ExternalTeamRef, payload.ExternalChannelRef} {
			if strings.TrimSpace(ref) == "" || len(ref) > 128 || strings.ContainsAny(ref, "\x00\r\n") {
				return commandOutcome{}, errs.ErrInvalid
			}
		}
		if !lowerHexDigest(payload.ExternalUserDigest) || payload.SubjectRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		if err := repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: payload.ConnectionRef}); err != nil {
			return commandOutcome{}, err
		}
		connection, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, payload.ConnectionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		if connection.definitionKey != "mattermost" || connection.lifecycleState != "ACTIVE" || !connection.enabled {
			return commandOutcome{}, errs.ErrForbidden
		}
		if connection.version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		ref, err := newRef("iid")
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		result = entity.InteractionIdentity{ConnectionRef: payload.ConnectionRef, ConnectionVersion: connection.version,
			ExternalTeamRef: payload.ExternalTeamRef, ExternalChannelRef: payload.ExternalChannelRef, ExternalUserDigest: payload.ExternalUserDigest, SubjectRef: payload.SubjectRef, State: "ACTIVE"}
		err = tx.QueryRow(ctx, queryInteractionIdentityInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID,
			"connection_id": connection.id, "connection_version": connection.version, "team_ref": payload.ExternalTeamRef,
			"channel_ref": payload.ExternalChannelRef, "user_digest": payload.ExternalUserDigest, "subject_ref": payload.SubjectRef, "actor_id": current.actorID}).Scan(&result.Ref, &result.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		var err error
		result, err = scanInteractionIdentity(tx.QueryRow(ctx, queryInteractionIdentityGet, current.organizationID, payload.IdentityRef))
		if err != nil {
			return commandOutcome{}, err
		}
		if err := repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: result.ConnectionRef}); err != nil {
			return commandOutcome{}, err
		}
		var ref string
		err = tx.QueryRow(ctx, queryInteractionIdentityRevoke, current.organizationID, result.Ref, *input.Mutation.ExpectedVersion, current.actorID).Scan(&ref)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		result, err = scanInteractionIdentity(tx.QueryRow(ctx, queryInteractionIdentityGet, current.organizationID, ref))
		if err != nil {
			return commandOutcome{}, err
		}
	}
	return commandOutcome{result: command.Result{InteractionIdentity: &result}, resourceKind: "INTERACTION_IDENTITY", resourceRef: result.Ref, summary: "i18n:INTERACTION_IDENTITY_CHANGED"}, nil
}

// ListInteractionIdentities uses a five-second read-only RepeatableRead transaction, rejects
// project-scoped authority, requires organization access.manage and pages SQL-visible mappings
// with an actor-bound catalog cursor. It closes/checks rows and commits before return;
// commit/backend failure is Unavailable and no mapping is modified.
// ListInteractionIdentities использует пятисекундную read-only RepeatableRead transaction,
// отклоняет project-scoped authority, требует organization access.manage и постранично читает
// SQL-visible mappings с actor-bound catalog cursor. Закрывает/проверяет rows и требует commit до
// возврата; commit/backend failure даёт Unavailable, mappings не меняются.
func (repository *Repository) ListInteractionIdentities(ctx context.Context, principal value.Principal, connection string, page query.Page) ([]entity.InteractionIdentity, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if current.authorityProjectID != "" {
		return nil, "", errs.ErrForbidden
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.requireAccess(ctx, tx, current, "access.manage", organizationTarget(current.organizationRef)); err != nil {
		return nil, "", err
	}
	filter := query.Filter{ResourceRef: connection, Page: page}
	cursor, err := decodeCatalogCursor(current, "INTERACTION_IDENTITY", filter)
	if err != nil {
		return nil, "", err
	}
	limit := boundedPage(page)
	rows, err := tx.Query(ctx, queryInteractionIdentityList, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "connection_ref": connection, "cursor_ref": cursor, "page_size": limit + 1})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	var result []entity.InteractionIdentity
	for rows.Next() {
		item, err := scanInteractionIdentity(rows)
		if err != nil {
			rows.Close()
			return nil, "", err
		}
		result = append(result, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(result) > int(limit) {
		result = result[:limit]
		next = encodeCatalogCursor(current, "INTERACTION_IDENTITY", filter, result[len(result)-1].Ref)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return result, next, nil
}

// resolveInteractionIdentity resolves exact external mapping inside the trusted tenant transaction
// and replaces actor ID/ref/name/role plus mapping ID on a scope copy. Missing mapping is
// Forbidden; backend failure Unavailable. It clears credential-authentication time so an
// interaction cannot inherit fresh interactive authentication; other trusted scope fields remain
// unchanged.
// resolveInteractionIdentity разрешает exact external mapping внутри trusted tenant transaction и
// заменяет actor ID/ref/name/role с mapping ID в scope copy. Missing mapping даёт Forbidden,
// backend failure — Unavailable. Очищает credential-authentication time, чтобы interaction не
// наследовала fresh interactive authentication; прочие trusted scope fields сохраняются.
func (repository *Repository) resolveInteractionIdentity(ctx context.Context, tx pgx.Tx, current scope, input command.InteractionMessageInput) (scope, error) {
	human := current
	err := tx.QueryRow(ctx, queryInteractionIdentityResolve, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"connection_ref": input.ConnectionRef, "team_ref": input.ExternalTeamRef, "channel_ref": input.ExternalChannelRef, "user_digest": input.ExternalUserDigest}).Scan(&human.actorID, &human.actorRef, &human.actorName, &human.role, &human.interactionIdentityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return scope{}, errs.ErrForbidden
	}
	if err != nil {
		return scope{}, errs.ErrUnavailable
	}
	human.credentialAuthenticatedAt = time.Time{}
	return human, nil
}
