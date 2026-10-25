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

// queryOwnerGateScope embeds exact tenant gate scope and owner organization/project, accepting
// organization gates only for the server-owned organization assistant lineage. It does not trust a
// scope label supplied by a caller.
// queryOwnerGateScope встраивает точные tenant gate scope и owner organization/project, допуская
// organization gates лишь для серверной organization assistant lineage. Не доверяет scope label
// caller.
//
//go:embed sql/owner_gate_scope.sql
var queryOwnerGateScope string

// projectOwnerGateScope resolves stored owner scope and rejects conflicting existing gate
// scope/organization/project as NotFound. Actor-scoped organization reads require a transaction
// and organization role-image access before filling scope fields; event projection does not invent
// actor authority. Query failures are Unavailable.
// projectOwnerGateScope разрешает stored owner scope и отклоняет противоречащие прежние gate
// scope/organization/project как NotFound. Actor-scoped organization reads требуют транзакцию и
// organization role-image access до заполнения scope fields; event projection не создаёт actor
// authority. Query failures дают Unavailable.
func (repository *Repository) projectOwnerGateScope(ctx context.Context, runner queryRunner, current scope, gate *entity.OwnerGate, actorScoped bool) error {
	var scopeKind, organizationRef, projectRef string
	err := runner.QueryRow(ctx, queryOwnerGateScope, current.organizationID, gate.Ref).Scan(&scopeKind, &organizationRef, &projectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if gate.ScopeKind != "" && gate.ScopeKind != scopeKind || gate.OrganizationRef != "" && gate.OrganizationRef != organizationRef || gate.ProjectRef != projectRef {
		return errs.ErrNotFound
	}
	if actorScoped && scopeKind == "ORGANIZATION" {
		tx, ok := runner.(pgx.Tx)
		if !ok {
			return errs.ErrUnavailable
		}
		if err := repository.requireOrganizationRoleImageAccess(ctx, tx, current); err != nil {
			return err
		}
	}
	gate.ScopeKind, gate.OrganizationRef = scopeKind, organizationRef
	return nil
}

// authorizeOwnerGateCommand requires typed nonempty gate identity, resolves owner
// organization/project scope and applies the organization boundary where needed before exact
// gate.resolve. Missing targets are NotFound and query failures Unavailable; payload project
// identifiers cannot override owner scope, while OCC/decision/lifecycle checks occur in the
// command handler.
// authorizeOwnerGateCommand требует typed непустую gate identity, разрешает owner
// organization/project scope и при необходимости применяет organization boundary до точного
// gate.resolve. Отсутствие target даёт NotFound, query failures — Unavailable; project identifiers
// payload не меняют owner scope, OCC/decision/lifecycle проверяет command handler.
func (repository *Repository) authorizeOwnerGateCommand(ctx context.Context, tx pgx.Tx, current scope, raw any) error {
	payload, ok := raw.(command.GateResolutionInput)
	if !ok || payload.GateRef == "" {
		return errs.ErrInvalid
	}
	var scopeKind, organizationRef, projectRef string
	err := tx.QueryRow(ctx, queryOwnerGateScope, current.organizationID, payload.GateRef).Scan(&scopeKind, &organizationRef, &projectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if scopeKind == "ORGANIZATION" {
		if err := repository.requireOrganizationRoleImageAccess(ctx, tx, current); err != nil {
			return err
		}
	}
	return repository.requireAccess(ctx, tx, current, "gate.resolve", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "OWNER_GATE", ResourceRef: payload.GateRef, ProjectRef: projectRef})
}
