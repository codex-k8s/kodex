package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/configuration_source__refresh_due.sql
var queryConfigurationSourceRefreshDue string

func (repository *Repository) refreshConfigurationSources(ctx context.Context, tx pgx.Tx, current scope, limit int32) error {
	rows, err := tx.Query(ctx, queryConfigurationSourceRefreshDue, current.organizationID, limit)
	if err != nil {
		return errs.ErrUnavailable
	}
	type dueSource struct{ ref, actor, organization string }
	due := []dueSource{}
	for rows.Next() {
		var item dueSource
		if rows.Scan(&item.ref, &item.actor, &item.organization) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		due = append(due, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	for _, item := range due {
		source, err := readConfigurationSource(ctx, tx, current.organizationID, item.ref)
		if err != nil {
			return errs.ErrUnavailable
		}
		var actor scope
		err = tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, item.actor, item.organization).Scan(&actor.organizationID, &actor.organizationRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return errs.ErrUnavailable
			}
			if _, err := repository.sourceState(ctx, tx, current, source, entity.ConfigurationSourceBlocked, entity.ConfigurationSourceAccess); err != nil {
				return err
			}
			continue
		}
		actor.correlationRef = current.correlationRef
		set, err := repository.resolveManagedSet(ctx, tx, actor, command.ManagedConfigurationInput{ConfigurationRef: item.ref}, "", false)
		if err != nil {
			return err
		}
		kind := command.RefreshIntegrationDefinitionGitSource
		if set.Kind == "ROLE_IMAGE" {
			kind = command.RefreshRoleImageGitSource
		}
		_, err = repository.changeConfigurationSource(ctx, tx, actor, command.Command{Kind: kind, Mutation: value.Mutation{ExpectedVersion: &set.Version}, Payload: command.ManagedConfigurationGitSourceInput{ConfigurationRef: item.ref}})
		if err != nil {
			if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrConflict) && !errors.Is(err, errs.ErrInvalid) {
				return err
			}
			if _, err := repository.sourceState(ctx, tx, actor, source, entity.ConfigurationSourceBlocked, entity.ConfigurationSourceAccess); err != nil {
				return err
			}
			continue
		}
		source, err = readConfigurationSource(ctx, tx, current.organizationID, item.ref)
		if err != nil {
			return errs.ErrUnavailable
		}
		if _, err := repository.sourceState(ctx, tx, actor, source, entity.ConfigurationSourceQueued, ""); err != nil {
			return err
		}
	}
	return nil
}
