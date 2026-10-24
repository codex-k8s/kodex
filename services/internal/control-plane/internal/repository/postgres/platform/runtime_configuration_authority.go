package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryRuntimeCatalogSystemAgent embeds exact tenant/ref existence of a nonarchived organization
// system assistant, separating it from ordinary project-agent runtime authority.
// queryRuntimeCatalogSystemAgent встраивает точную tenant/ref проверку существования неархивного
// organization system assistant, отделяя его от обычной project-agent runtime authority.
//
//go:embed sql/runtime_catalog__system_agent.sql
var queryRuntimeCatalogSystemAgent string

// resolveRuntimeConfigurationTarget checks canonical system-assistant identity and resolves
// organization.manage for SYSTEM or the requested ordinary AGENT permission otherwise. SQL failure
// is Unavailable; project permissions cannot authorize organization runtime configuration, and no
// permission is granted here.
// resolveRuntimeConfigurationTarget проверяет каноническую system-assistant identity и разрешает
// organization.manage для SYSTEM, иначе запрошенное обычное AGENT permission. SQL failure даёт
// Unavailable; project permissions не разрешают organization runtime configuration, а новые
// полномочия здесь не выдаются.
func (repository *Repository) resolveRuntimeConfigurationTarget(ctx context.Context, tx pgx.Tx, current scope, permission, ref string) (string, resolvedAccessTarget, error) {
	var system bool
	if err := tx.QueryRow(ctx, queryRuntimeCatalogSystemAgent, current.organizationID, ref).Scan(&system); err != nil {
		return "", resolvedAccessTarget{}, errs.ErrUnavailable
	}
	if system {
		return repository.resolveCommandTarget(ctx, tx, current, "organization.manage", "ORGANIZATION", current.organizationRef, "")
	}
	return repository.resolveCommandTarget(ctx, tx, current, permission, "AGENT", ref, "")
}
