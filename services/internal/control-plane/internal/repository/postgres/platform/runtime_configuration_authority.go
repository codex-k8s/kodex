package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_catalog__system_agent.sql
var queryRuntimeCatalogSystemAgent string

// Системная конфигурация принадлежит организации; право на проектного Agent
// не переносится на системного ассистента.
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
