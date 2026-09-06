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

//go:embed sql/interaction_identity_insert.sql
var queryInteractionIdentityInsert string

//go:embed sql/interaction_identity_get.sql
var queryInteractionIdentityGet string

//go:embed sql/interaction_identity_revoke.sql
var queryInteractionIdentityRevoke string

//go:embed sql/interaction_identity_list.sql
var queryInteractionIdentityList string

//go:embed sql/interaction_identity_resolve.sql
var queryInteractionIdentityResolve string

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
