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

// queryOrganizationRoleImageResolveRole resolves the canonical organization system-assistant role
// only through matching assistant_runtime, agent and role owner rows with no project; payload role
// IDs do not select this parent.
// queryOrganizationRoleImageResolveRole разрешает каноническую роль system assistant организации
// только по совпадающим owner строкам assistant_runtime, agent и role без проекта; role IDs из
// payload не выбирают родителя.
//
//go:embed sql/organization_role_images__resolve_role.sql
var queryOrganizationRoleImageResolveRole string

// queryOrganizationRoleImageInsertConfiguration inserts an organization-owned UI ROLE_IMAGE
// managed set with NULL project and control-center source, returning the canonical managed-set
// projection with empty project/current-revision fields.
// queryOrganizationRoleImageInsertConfiguration вставляет принадлежащий организации UI ROLE_IMAGE
// managed set с NULL project и source control-center, возвращая каноническое managed-set
// представление с пустыми project/current-revision fields.
//
//go:embed sql/organization_role_images__insert_configuration.sql
var queryOrganizationRoleImageInsertConfiguration string

// createOrganizationRoleImageConfiguration allocates a server managed-set reference and inserts
// the organization UI configuration with the current actor in the caller transaction, preserving
// unavailable allocation failure and canonical write-error mapping without committing.
// createOrganizationRoleImageConfiguration назначает server managed-set reference и вставляет UI
// configuration организации с текущим actor в транзакции вызывающего метода, сохраняя unavailable
// при отказе генерации и канонический write-error mapping без commit.
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

// roleImageReadPermission maps exact ORGANIZATION scope to organization.manage and all other
// inputs to project.view; this permission selector does not validate scope or authorize a
// resource.
// roleImageReadPermission сопоставляет точному ORGANIZATION scope разрешение organization.manage,
// остальным входам — project.view; выбор permission не валидирует scope и не авторизует ресурс.
func roleImageReadPermission(scopeKind string) string {
	if scopeKind == "ORGANIZATION" {
		return "organization.manage"
	}
	return "project.view"
}

// requireOrganizationRoleImageAccess hides organization images from project-bound principals and
// roles other than OWNER/ADMINISTRATOR with NotFound, then requires current organization.manage on
// the authoritative organization target.
// requireOrganizationRoleImageAccess скрывает образы организации от project-bound principals и
// ролей кроме OWNER/ADMINISTRATOR ответом NotFound, затем требует текущий organization.manage для
// авторитетного organization target.
func (repository *Repository) requireOrganizationRoleImageAccess(ctx context.Context, tx pgx.Tx, current scope) error {
	if current.authorityProjectID != "" || current.role != "OWNER" && current.role != "ADMINISTRATOR" {
		return errs.ErrNotFound
	}
	return repository.requireAccess(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef))
}

// ListOrganization rejects project and role-definition filters, forces ORGANIZATION scope and
// delegates listing/count/cursor/redaction to the common scoped reader; empty project fields do
// not themselves establish organization ownership.
// ListOrganization отклоняет project и role-definition filters, принудительно задаёт ORGANIZATION
// scope и передаёт listing/count/cursor/redaction общему scoped reader; пустые project fields сами
// не устанавливают organization ownership.
func (repository *Repository) ListOrganization(ctx context.Context, principal value.Principal, filter roleimagerepo.Filter) ([]entity.RoleImageRecipe, string, int64, error) {
	if filter.ProjectRef != "" || filter.RoleDefinitionRef != "" {
		return nil, "", 0, errs.ErrInvalid
	}
	filter.ScopeKind = "ORGANIZATION"
	return repository.listRoleImages(ctx, principal, filter)
}

// GetOrganization selects the explicit ORGANIZATION detail path, retaining its current
// organization/source authorization and source redaction instead of using the project endpoint.
// GetOrganization выбирает явный ORGANIZATION detail path, сохраняя текущие organization/source
// authorization и source redaction вместо project endpoint.
func (repository *Repository) GetOrganization(ctx context.Context, principal value.Principal, ref string) (roleimagerepo.Detail, error) {
	return repository.getScopedRoleImage(ctx, principal, ref, "ORGANIZATION")
}

// ListOrganizationRevisions resolves scope in a read-only RepeatableRead transaction, requires
// organization owner access and exact image.source.view before delegating paged immutable revision
// reads. The delegated reader performs successful commit; deferred rollback covers failures and
// does not grant write authority.
// ListOrganizationRevisions разрешает scope в read-only RepeatableRead транзакции, требует
// organization owner access и точный image.source.view до делегирования постраничного чтения
// immutable revisions. Делегированный reader делает успешный commit; deferred rollback покрывает
// ошибки и не выдаёт полномочий записи.
func (repository *Repository) ListOrganizationRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.RoleImageRecipeRevision, string, error) {
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return nil, "", err
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

// ManageOrganization rejects project/role-definition locators and forces ORGANIZATION before the
// canonical management transaction, retaining fresh owner checks, OCC, idempotency and source
// handling there.
// ManageOrganization отклоняет project/role-definition locators и задаёт ORGANIZATION до
// канонической management transaction, сохраняя там свежие owner checks, OCC, idempotency и source
// handling.
func (repository *Repository) ManageOrganization(ctx context.Context, input roleimagerepo.ManageInput) (roleimagerepo.ManageResult, error) {
	if input.ProjectRef != "" || input.RoleDefinitionRef != "" {
		return roleimagerepo.ManageResult{}, errs.ErrInvalid
	}
	input.ScopeKind = "ORGANIZATION"
	return repository.manageScopedRoleImage(ctx, input)
}

// RequestOrganizationPromotion forces ORGANIZATION scope and retries the complete canonical
// promotion-request transaction under the existing bounded policy; exact owner, recipe/artifact
// and evidence checks remain in that transaction.
// RequestOrganizationPromotion задаёт ORGANIZATION scope и повторяет полную каноническую
// promotion-request transaction по прежней ограниченной policy; точные owner, recipe/artifact и
// evidence checks остаются в ней.
func (repository *Repository) RequestOrganizationPromotion(ctx context.Context, input roleimagerepo.PromotionRequestInput) (entity.RoleImagePromotionReceipt, error) {
	input.ScopeKind = "ORGANIZATION"
	return retryRoleImageTransaction(ctx, func() (entity.RoleImagePromotionReceipt, error) { return repository.requestPromotion(ctx, input) })
}
