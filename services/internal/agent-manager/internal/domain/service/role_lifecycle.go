package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type roleCommandPayload struct {
	RoleProfile entity.RoleProfile `json:"role_profile"`
}

func (s *Service) CreateRoleProfile(ctx context.Context, input CreateRoleProfileInput) (entity.RoleProfile, error) {
	if err := s.requireRepository(); err != nil {
		return entity.RoleProfile{}, err
	}
	if err := validateScope(input.Scope); err != nil {
		return entity.RoleProfile{}, err
	}
	if err := validateSlug(input.Slug); err != nil {
		return entity.RoleProfile{}, err
	}
	if strings.TrimSpace(input.RuntimeProfile) == "" {
		return entity.RoleProfile{}, errs.ErrInvalidArgument
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationCreateRoleProfile, enum.CommandAggregateTypeRoleProfile, roleFromPayload, verifyScopedReplay(uuid.Nil, &input.Scope, s.repository.GetRoleProfile, roleID, roleScope)); ok || err != nil {
		return replay, err
	}
	now := s.clock.Now()
	role := entity.RoleProfile{
		VersionedBase:            entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Scope:                    input.Scope,
		Slug:                     strings.TrimSpace(input.Slug),
		DisplayName:              input.DisplayName,
		IconObjectURI:            strings.TrimSpace(input.IconObjectURI),
		RoleKind:                 input.RoleKind,
		RuntimeProfile:           strings.TrimSpace(input.RuntimeProfile),
		AllowedMCPTools:          input.AllowedMCPTools,
		ProviderAccountPolicyRef: strings.TrimSpace(input.ProviderAccountPolicyRef),
		Status:                   enum.RoleStatusDraft,
	}
	payload, err := marshalCommandPayload(roleCommandPayload{RoleProfile: role})
	if err != nil {
		return entity.RoleProfile{}, err
	}
	result, err := commandResult(input.Meta, operationCreateRoleProfile, enum.CommandAggregateTypeRoleProfile, role.ID, payload, now)
	if err != nil {
		return entity.RoleProfile{}, err
	}
	return role, s.repository.CreateRoleProfileWithResult(ctx, role, result)
}

func (s *Service) UpdateRoleProfile(ctx context.Context, input UpdateRoleProfileInput) (entity.RoleProfile, error) {
	if err := s.requireRepository(); err != nil {
		return entity.RoleProfile{}, err
	}
	if err := validateID(input.RoleProfileID); err != nil {
		return entity.RoleProfile{}, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.RoleProfile{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationUpdateRoleProfile, enum.CommandAggregateTypeRoleProfile, roleFromPayload, verifyScopedReplay(input.RoleProfileID, nil, s.repository.GetRoleProfile, roleID, roleScope)); ok || err != nil {
		return replay, err
	}
	role, err := s.repository.GetRoleProfile(ctx, input.RoleProfileID)
	if err != nil {
		return entity.RoleProfile{}, err
	}
	if role.Version != previousVersion {
		return entity.RoleProfile{}, errs.ErrConflict
	}
	now := s.clock.Now()
	previousStatus := role.Status
	role.DisplayName = input.DisplayName
	role.IconObjectURI = strings.TrimSpace(input.IconObjectURI)
	if input.RoleKind != "" {
		role.RoleKind = input.RoleKind
	}
	if strings.TrimSpace(input.RuntimeProfile) != "" {
		role.RuntimeProfile = strings.TrimSpace(input.RuntimeProfile)
	}
	role.AllowedMCPTools = input.AllowedMCPTools
	role.ProviderAccountPolicyRef = strings.TrimSpace(input.ProviderAccountPolicyRef)
	if input.Status != "" {
		role.Status = input.Status
	}
	role.Version++
	role.UpdatedAt = now
	payload, err := marshalCommandPayload(roleCommandPayload{RoleProfile: role})
	if err != nil {
		return entity.RoleProfile{}, err
	}
	result, err := commandResult(input.Meta, operationUpdateRoleProfile, enum.CommandAggregateTypeRoleProfile, role.ID, payload, now)
	if err != nil {
		return entity.RoleProfile{}, err
	}
	var event *entity.OutboxEvent
	if previousStatus != enum.RoleStatusActive && role.Status == enum.RoleStatusActive {
		activationEvent, err := roleActivatedEvent(s.idGenerator.New(), role, now)
		if err != nil {
			return entity.RoleProfile{}, err
		}
		event = &activationEvent
	}
	return role, s.repository.UpdateRoleProfileWithResult(ctx, role, previousVersion, result, event)
}

func (s *Service) GetRoleProfile(ctx context.Context, id uuid.UUID) (entity.RoleProfile, error) {
	return getByID(ctx, s, id, s.getRoleFromRepository)
}

func (s *Service) ListRoleProfiles(ctx context.Context, filter query.RoleProfileFilter) ([]entity.RoleProfile, value.PageResult, error) {
	return listFromRepository(ctx, s, filter, s.listRolesFromRepository)
}

func (s *Service) getRoleFromRepository(ctx context.Context, id uuid.UUID) (entity.RoleProfile, error) {
	return s.repository.GetRoleProfile(ctx, id)
}

func (s *Service) listRolesFromRepository(ctx context.Context, filter query.RoleProfileFilter) ([]entity.RoleProfile, value.PageResult, error) {
	return s.repository.ListRoleProfiles(ctx, filter)
}

func roleFromPayload(payload []byte) (entity.RoleProfile, error) {
	var result roleCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.RoleProfile, err
}
