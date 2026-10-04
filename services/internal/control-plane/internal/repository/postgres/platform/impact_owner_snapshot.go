package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/impact_consumer_owner.sql
var queryImpactConsumerOwner string

func validRuntimeOwnerSnapshot(scopeKind, organizationRef, projectRef string) bool {
	return organizationRef != "" && (scopeKind == "ORGANIZATION" && projectRef == "" || scopeKind == "PROJECT" && projectRef != "")
}

// Ожидаемый tuple является только immutable snapshot, а не источником прав.
func matchRuntimeOwnerSnapshot(scopeKind, organizationRef, projectRef, expectedScope, expectedOrganization, expectedProject string) error {
	if !validRuntimeOwnerSnapshot(expectedScope, expectedOrganization, expectedProject) {
		return errs.ErrInvalid
	}
	if !validRuntimeOwnerSnapshot(scopeKind, organizationRef, projectRef) || scopeKind != expectedScope || organizationRef != expectedOrganization || projectRef != expectedProject {
		return errs.ErrNotFound
	}
	return nil
}

func (r *Repository) impactConsumerOwner(ctx context.Context, tx pgx.Tx, s scope, kind, ref string) (entity.RuntimeEnvironmentConsumer, error) {
	var owner entity.RuntimeEnvironmentConsumer
	if kind == "AGENT_CONTINUATION" {
		kind = "AGENT"
	}
	err := tx.QueryRow(ctx, queryImpactConsumerOwner, s.organizationID, kind, ref).Scan(&owner.ScopeKind, &owner.OrganizationRef, &owner.ProjectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner, errs.ErrNotFound
	}
	if err != nil || !validRuntimeOwnerSnapshot(owner.ScopeKind, owner.OrganizationRef, owner.ProjectRef) || owner.OrganizationRef != s.organizationRef {
		return owner, errs.ErrUnavailable
	}
	if owner.ScopeKind == "ORGANIZATION" {
		if err := r.requireRuntimeEnvironmentOwnerAccess(ctx, tx, s, owner.ScopeKind, owner.ProjectRef); err != nil {
			return owner, err
		}
	}
	return owner, nil
}

func (r *Repository) authorizeRuntimeEnvironmentConsumers(ctx context.Context, tx pgx.Tx, s scope, environment entity.RuntimeEnvironmentSet, consumers []entity.RuntimeEnvironmentConsumer) error {
	if err := r.requireRuntimeEnvironmentOwnerAccess(ctx, tx, s, environment.ScopeKind, environment.ProjectRef); err != nil {
		return err
	}
	for _, consumer := range consumers {
		owner, err := r.impactConsumerOwner(ctx, tx, s, "AGENT", consumer.AgentRef)
		if err != nil {
			return err
		}
		permission, target, err := r.resolveRuntimeConfigurationTarget(ctx, tx, s, "agent.manage", consumer.AgentRef)
		if err != nil {
			return err
		}
		if err := r.requireAccess(ctx, tx, s, permission, target); err != nil {
			return err
		}
		if err := matchRuntimeOwnerSnapshot(owner.ScopeKind, owner.OrganizationRef, owner.ProjectRef, consumer.ScopeKind, consumer.OrganizationRef, consumer.ProjectRef); err != nil {
			return err
		}
		if err := matchRuntimeOwnerSnapshot(environment.ScopeKind, environment.OrganizationRef, environment.ProjectRef, owner.ScopeKind, owner.OrganizationRef, owner.ProjectRef); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) authorizeRuntimeSecretSelections(ctx context.Context, tx pgx.Tx, s scope, secretRef string, revision int64, selections []entity.RuntimeSecretRebindSelection) (lockedRuntimeSecret, error) {
	secret, err := r.lockRuntimeSecret(ctx, tx, s.organizationID, secretRef)
	if err != nil {
		return secret, err
	}
	if _, _, err := r.secretImpactTarget(ctx, tx, s, secretRef, revision); err != nil {
		return secret, err
	}
	for _, selection := range selections {
		environment, err := r.getRuntimeEnvironmentTx(ctx, tx, s, selection.EnvironmentRef)
		if err != nil {
			return secret, err
		}
		if err := r.requireRuntimeEnvironmentOwnerAccess(ctx, tx, s, environment.ScopeKind, environment.ProjectRef); err != nil {
			return secret, err
		}
		if err := matchRuntimeOwnerSnapshot(environment.ScopeKind, environment.OrganizationRef, environment.ProjectRef, selection.ScopeKind, selection.OrganizationRef, selection.ProjectRef); err != nil {
			return secret, err
		}
		if err := matchRuntimeOwnerSnapshot(secret.scopeKind, secret.organizationRef, secret.projectRef, environment.ScopeKind, environment.OrganizationRef, environment.ProjectRef); err != nil {
			return secret, err
		}
		if err := r.authorizeRuntimeEnvironmentConsumers(ctx, tx, s, environment, selection.Consumers); err != nil {
			return secret, err
		}
	}
	return secret, nil
}

func (r *Repository) validateImpactOwnerReceipt(ctx context.Context, tx pgx.Tx, s scope, input command.Command, result command.Result) error {
	if result.RevisionImpactPlan != nil {
		row, err := r.revisionImpact(ctx, tx, s, result.RevisionImpactPlan.Ref)
		if err != nil {
			return err
		}
		if err := r.revisionImpactAccess(ctx, tx, s, row); err != nil {
			return err
		}
		items, err := r.revisionImpactItems(ctx, tx, row, s.actorID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if !validRuntimeOwnerSnapshot(item.ScopeKind, item.OrganizationRef, item.ProjectRef) {
				return errs.ErrNotFound
			}
		}
	}
	if result.RoleImageImpactPlan != nil {
		row, err := r.roleImageImpact(ctx, tx, s, result.RoleImageImpactPlan.Ref)
		if err != nil {
			return err
		}
		if _, _, err := r.roleImageImpactAccess(ctx, tx, s, row); err != nil {
			return err
		}
		items, err := r.roleImageImpactItems(ctx, tx, row.id)
		if err != nil {
			return err
		}
		for _, item := range items {
			if !validRuntimeOwnerSnapshot(item.Consumer.ScopeKind, item.Consumer.OrganizationRef, item.Consumer.ProjectRef) {
				return errs.ErrNotFound
			}
		}
	}
	environments := append([]entity.RuntimeEnvironmentSet{}, result.RuntimeEnvironments...)
	if result.RuntimeEnvironment != nil {
		environments = append(environments, *result.RuntimeEnvironment)
	}
	for _, item := range environments {
		environment, err := r.getRuntimeEnvironmentTx(ctx, tx, s, item.Ref)
		if err != nil {
			return err
		}
		if err := matchRuntimeOwnerSnapshot(environment.ScopeKind, environment.OrganizationRef, environment.ProjectRef, item.ScopeKind, item.OrganizationRef, item.ProjectRef); err != nil {
			return errs.ErrNotFound
		}
	}
	return nil
}
