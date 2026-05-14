package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func verifyScopedReplay[T any](
	expectedID uuid.UUID,
	expectedScope *value.ScopeRef,
	load func(context.Context, uuid.UUID) (T, error),
	idOf func(T) uuid.UUID,
	scopeOf func(T) value.ScopeRef,
) func(context.Context, entity.CommandResult, T) error {
	return verifyReplay(expectedID, load, idOf, func(stored T) error {
		if expectedScope == nil {
			return nil
		}
		if !sameScope(scopeOf(stored), *expectedScope) {
			return errs.ErrConflict
		}
		return nil
	})
}

func verifyPromptReplay[T any](
	expectedID uuid.UUID,
	expectedRoleID uuid.UUID,
	expectedKind enum.PromptKind,
	load func(context.Context, uuid.UUID) (T, error),
	idOf func(T) uuid.UUID,
	roleIDOf func(T) uuid.UUID,
	kindOf func(T) enum.PromptKind,
) func(context.Context, entity.CommandResult, T) error {
	return verifyReplay(expectedID, load, idOf, func(stored T) error {
		if expectedRoleID != uuid.Nil && roleIDOf(stored) != expectedRoleID {
			return errs.ErrConflict
		}
		if expectedKind != "" && kindOf(stored) != expectedKind {
			return errs.ErrConflict
		}
		return nil
	})
}

func verifyReplay[T any](
	expectedID uuid.UUID,
	load func(context.Context, uuid.UUID) (T, error),
	idOf func(T) uuid.UUID,
	validateStored func(T) error,
) func(context.Context, entity.CommandResult, T) error {
	return func(ctx context.Context, result entity.CommandResult, replay T) error {
		if expectedID != uuid.Nil && result.AggregateID != expectedID {
			return errs.ErrConflict
		}
		stored, err := load(ctx, result.AggregateID)
		if err != nil {
			return err
		}
		if idOf(replay) != idOf(stored) {
			return errs.ErrConflict
		}
		return validateStored(stored)
	}
}

func flowID(flow entity.Flow) uuid.UUID { return flow.ID }

func flowScope(flow entity.Flow) value.ScopeRef { return flow.Scope }

func flowVersionID(version entity.FlowVersion) uuid.UUID { return version.ID }

func requireFlowID(expectedFlowID uuid.UUID) func(entity.FlowVersion) error {
	return func(version entity.FlowVersion) error {
		if expectedFlowID != uuid.Nil && version.FlowID != expectedFlowID {
			return errs.ErrConflict
		}
		return nil
	}
}

func acceptAnyFlowID(entity.FlowVersion) error { return nil }

func roleID(role entity.RoleProfile) uuid.UUID { return role.ID }

func roleScope(role entity.RoleProfile) value.ScopeRef { return role.Scope }

func promptTemplateID(template entity.PromptTemplate) uuid.UUID { return template.ID }

func promptTemplateRoleID(template entity.PromptTemplate) uuid.UUID { return template.RoleProfileID }

func promptTemplateKind(template entity.PromptTemplate) enum.PromptKind { return template.PromptKind }

func promptVersionID(version entity.PromptTemplateVersion) uuid.UUID { return version.ID }

func promptVersionRoleID(version entity.PromptTemplateVersion) uuid.UUID {
	return version.RoleProfileID
}

func promptVersionKind(version entity.PromptTemplateVersion) enum.PromptKind {
	return version.PromptKind
}
