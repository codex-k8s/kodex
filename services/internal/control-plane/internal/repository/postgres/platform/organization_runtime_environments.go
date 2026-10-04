package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) requireRuntimeEnvironmentOwnerAccess(ctx context.Context, tx pgx.Tx, s scope, scopeKind, projectRef string) error {
	if scopeKind == "ORGANIZATION" {
		if projectRef != "" {
			return errs.ErrNotFound
		}
		return r.requireOrganizationRuntimeResourceAccess(ctx, tx, s, "organization.manage")
	}
	if scopeKind != "PROJECT" || projectRef == "" {
		return errs.ErrNotFound
	}
	target, err := r.resolveAccessTarget(ctx, tx, s.organizationID, entity.AccessScope{ResourceKind: "PROJECT", ResourceRef: projectRef})
	if err != nil {
		return err
	}
	if s.authorityProjectID != "" && s.authorityProjectID != target.projectID {
		return errs.ErrNotFound
	}
	return r.requireAccess(ctx, tx, s, "project.manage", target)
}
