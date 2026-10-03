package platform

import (
	"context"
	_ "embed"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/organization_role_images__resolve_role.sql
var queryOrganizationRoleImageResolveRole string

//go:embed sql/organization_role_images__insert_configuration.sql
var queryOrganizationRoleImageInsertConfiguration string

func (repository *Repository) createOrganizationRoleImageConfiguration(ctx context.Context, tx pgx.Tx, current scope, name string) (managedSet, error) {
	ref, err := newRef("mcfg")
	if err != nil {
		return managedSet{}, errs.ErrUnavailable
	}
	item, err := scanManagedSet(tx.QueryRow(ctx, queryOrganizationRoleImageInsertConfiguration,
		ref, current.organizationID, name, current.actorID))
	if err != nil {
		return managedSet{}, mapWriteError(err)
	}
	return item, nil
}

func roleImageReadPermission(scopeKind string) string {
	if scopeKind == "ORGANIZATION" {
		return "organization.manage"
	}
	return "project.view"
}

func (repository *Repository) requireOrganizationRoleImageAccess(ctx context.Context, tx pgx.Tx, current scope) error {
	if current.authorityProjectID != "" || current.role != "OWNER" && current.role != "ADMINISTRATOR" {
		return errs.ErrNotFound
	}
	return repository.requireAccess(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef))
}

func (repository *Repository) ListOrganization(ctx context.Context, principal value.Principal, filter roleimagerepo.Filter) ([]entity.RoleImageRecipe, string, int64, error) {
	if filter.ProjectRef != "" || filter.RoleDefinitionRef != "" {
		return nil, "", 0, errs.ErrInvalid
	}
	filter.ScopeKind = "ORGANIZATION"
	return repository.listRoleImages(ctx, principal, filter)
}

func (repository *Repository) GetOrganization(ctx context.Context, principal value.Principal, ref string) (roleimagerepo.Detail, error) {
	return repository.getScopedRoleImage(ctx, principal, ref, "ORGANIZATION")
}

func (repository *Repository) ListOrganizationRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.RoleImageRecipeRevision, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.requireOrganizationRoleImageAccess(ctx, tx, current); err != nil {
		return nil, "", err
	}
	target, err := repository.resolveScopedRoleImageAccessTarget(ctx, tx, current, ref, "", "ORGANIZATION")
	if err != nil {
		return nil, "", err
	}
	authorization, err := repository.loadRoleImageAccessContext(ctx, tx, current)
	if err != nil {
		return nil, "", err
	}
	if !authorization.allowed("image.source.view", target) {
		return nil, "", errs.ErrNotFound
	}
	return repository.listRoleImageRevisions(ctx, tx, current, query.Filter{ResourceRef: ref, Page: page})
}

func (repository *Repository) ManageOrganization(ctx context.Context, input roleimagerepo.ManageInput) (roleimagerepo.ManageResult, error) {
	if input.ProjectRef != "" || input.RoleDefinitionRef != "" {
		return roleimagerepo.ManageResult{}, errs.ErrInvalid
	}
	input.ScopeKind = "ORGANIZATION"
	return repository.manageScopedRoleImage(ctx, input)
}

func (repository *Repository) RequestOrganizationPromotion(ctx context.Context, input roleimagerepo.PromotionRequestInput) (entity.RoleImagePromotionReceipt, error) {
	input.ScopeKind = "ORGANIZATION"
	return retryRoleImageTransaction(ctx, func() (entity.RoleImagePromotionReceipt, error) { return repository.requestPromotion(ctx, input) })
}
