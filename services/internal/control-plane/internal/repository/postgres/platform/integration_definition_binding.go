package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// attachIntegrationDefinitionBinding вызывается только адресным чтением после
// connection eligibility и использует тот же repeatable-read snapshot.
func (repository *Repository) attachIntegrationDefinitionBinding(ctx context.Context, tx pgx.Tx, current scope, item *entity.IntegrationConnection) error {
	if err := repository.requireAccess(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef)); err != nil {
		if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) {
			return nil
		}
		return err
	}
	arguments := pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "configuration_kind": "INTEGRATION_DEFINITION",
		"consumer_kind": "INTEGRATION_CONNECTION", "consumer_ref": item.Ref,
	}
	binding, err := scanManagedBinding(tx.QueryRow(ctx, queryManagedConfigurationGetConsumerBinding, arguments))
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err := tx.QueryRow(ctx, queryManagedConfigurationConsumerBindingExists, arguments).Scan(&exists); err != nil || exists {
			return errs.ErrUnavailable
		}
		item.DefinitionConfigurationBinding = &entity.IntegrationDefinitionConfigurationBinding{State: "ABSENT"}
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	set := managedSet{ManagedConfigurationSet: binding.Configuration}
	if err := repository.requireManagedSetAccess(ctx, tx, current, set, "project.view", "organization.view"); err != nil {
		if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) {
			return nil
		}
		return err
	}
	if binding.Configuration.Ref == "" || binding.Revision.Ref == "" || binding.Version < 1 {
		return errs.ErrUnavailable
	}
	item.DefinitionConfigurationBinding = &entity.IntegrationDefinitionConfigurationBinding{
		State: "MATCH", ConfigurationRef: binding.Configuration.Ref,
		RevisionRef: binding.Revision.Ref, BindingVersion: binding.Version,
	}
	return nil
}
