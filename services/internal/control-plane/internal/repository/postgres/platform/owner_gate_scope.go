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

//go:embed sql/owner_gate_scope.sql
var queryOwnerGateScope string

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
