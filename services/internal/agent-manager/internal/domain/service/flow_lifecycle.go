package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type flowCommandPayload struct {
	Flow entity.Flow `json:"flow"`
}

type flowVersionCommandPayload struct {
	FlowVersion entity.FlowVersion `json:"flow_version"`
}

func (s *Service) CreateFlow(ctx context.Context, input CreateFlowInput) (entity.Flow, error) {
	if err := s.requireRepository(); err != nil {
		return entity.Flow{}, err
	}
	if err := validateScope(input.Scope); err != nil {
		return entity.Flow{}, err
	}
	if err := validateSlug(input.Slug); err != nil {
		return entity.Flow{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationCreateFlow, enum.CommandAggregateTypeFlow, flowFromPayload, verifyScopedReplay(uuid.Nil, &input.Scope, s.repository.GetFlow, flowID, flowScope)); ok || err != nil {
		return replay, err
	}
	now := s.clock.Now()
	flow := entity.Flow{
		VersionedBase: entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Scope:         input.Scope,
		Slug:          strings.TrimSpace(input.Slug),
		DisplayName:   input.DisplayName,
		Description:   input.Description,
		IconObjectURI: strings.TrimSpace(input.IconObjectURI),
		Status:        enum.FlowStatusDraft,
	}
	payload, err := marshalCommandPayload(flowCommandPayload{Flow: flow})
	if err != nil {
		return entity.Flow{}, err
	}
	result, err := commandResult(input.Meta, operationCreateFlow, enum.CommandAggregateTypeFlow, flow.ID, payload, now)
	if err != nil {
		return entity.Flow{}, err
	}
	return flow, s.repository.CreateFlowWithResult(ctx, flow, result)
}

func (s *Service) UpdateFlow(ctx context.Context, input UpdateFlowInput) (entity.Flow, error) {
	if err := s.requireRepository(); err != nil {
		return entity.Flow{}, err
	}
	if err := validateID(input.FlowID); err != nil {
		return entity.Flow{}, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.Flow{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationUpdateFlow, enum.CommandAggregateTypeFlow, flowFromPayload, verifyScopedReplay(input.FlowID, nil, s.repository.GetFlow, flowID, flowScope)); ok || err != nil {
		return replay, err
	}
	stored, err := s.repository.GetFlow(ctx, input.FlowID)
	if err != nil {
		return entity.Flow{}, err
	}
	if stored.Version != previousVersion {
		return entity.Flow{}, errs.ErrConflict
	}
	now := s.clock.Now()
	stored.DisplayName = input.DisplayName
	stored.Description = input.Description
	stored.IconObjectURI = strings.TrimSpace(input.IconObjectURI)
	if input.Status != "" {
		stored.Status = input.Status
	}
	stored.Version++
	stored.UpdatedAt = now
	payload, err := marshalCommandPayload(flowCommandPayload{Flow: stored})
	if err != nil {
		return entity.Flow{}, err
	}
	result, err := commandResult(input.Meta, operationUpdateFlow, enum.CommandAggregateTypeFlow, stored.ID, payload, now)
	if err != nil {
		return entity.Flow{}, err
	}
	return stored, s.repository.UpdateFlowWithResult(ctx, stored, previousVersion, result)
}

func (s *Service) GetFlow(ctx context.Context, id uuid.UUID) (entity.Flow, error) {
	return getByID(ctx, s, id, s.getFlowFromRepository)
}

func (s *Service) ListFlows(ctx context.Context, filter query.FlowFilter) ([]entity.Flow, value.PageResult, error) {
	return listFromRepository(ctx, s, filter, s.listFlowsFromRepository)
}

func (s *Service) getFlowFromRepository(ctx context.Context, id uuid.UUID) (entity.Flow, error) {
	return s.repository.GetFlow(ctx, id)
}

func (s *Service) listFlowsFromRepository(ctx context.Context, filter query.FlowFilter) ([]entity.Flow, value.PageResult, error) {
	return s.repository.ListFlows(ctx, filter)
}

func (s *Service) CreateFlowVersion(ctx context.Context, input CreateFlowVersionInput) (entity.FlowVersion, error) {
	if err := s.requireRepository(); err != nil {
		return entity.FlowVersion{}, err
	}
	if err := validateID(input.FlowID); err != nil {
		return entity.FlowVersion{}, err
	}
	if strings.TrimSpace(input.DefinitionDigest) == "" {
		return entity.FlowVersion{}, errs.ErrInvalidArgument
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationCreateFlowVersion, enum.CommandAggregateTypeFlowVersion, flowVersionFromPayload, verifyReplay(uuid.Nil, s.repository.GetFlowVersion, flowVersionID, requireFlowID(input.FlowID))); ok || err != nil {
		return replay, err
	}
	now := s.clock.Now()
	version, err := s.buildFlowVersion(input, now)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	nextVersion, err := s.nextFlowVersion(ctx, input.FlowID)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	version.Version = nextVersion
	payload, err := marshalCommandPayload(flowVersionCommandPayload{FlowVersion: version})
	if err != nil {
		return entity.FlowVersion{}, err
	}
	result, err := commandResult(input.Meta, operationCreateFlowVersion, enum.CommandAggregateTypeFlowVersion, version.ID, payload, now)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	return s.repository.CreateFlowVersionWithResult(ctx, version, result)
}

func (s *Service) ActivateFlowVersion(ctx context.Context, input ActivateFlowVersionInput) (entity.FlowVersion, error) {
	if err := s.requireRepository(); err != nil {
		return entity.FlowVersion{}, err
	}
	if err := validateID(input.FlowVersionID); err != nil {
		return entity.FlowVersion{}, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationActivateFlowVersion, enum.CommandAggregateTypeFlowVersion, flowVersionFromPayload, verifyReplay(input.FlowVersionID, s.repository.GetFlowVersion, flowVersionID, acceptAnyFlowID)); ok || err != nil {
		return replay, err
	}
	version, err := s.repository.GetFlowVersion(ctx, input.FlowVersionID)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	flow, err := s.repository.GetFlow(ctx, version.FlowID)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	if flow.Version != previousVersion {
		return entity.FlowVersion{}, errs.ErrConflict
	}
	now := s.clock.Now()
	version.Status = enum.FlowVersionStatusActive
	version.ActivatedAt = &now
	flow.ActiveVersionID = &version.ID
	flow.Status = enum.FlowStatusActive
	flow.Version++
	flow.UpdatedAt = now
	payload, err := marshalCommandPayload(flowVersionCommandPayload{FlowVersion: version})
	if err != nil {
		return entity.FlowVersion{}, err
	}
	result, err := commandResult(input.Meta, operationActivateFlowVersion, enum.CommandAggregateTypeFlowVersion, version.ID, payload, now)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	event, err := flowActivatedEvent(s.idGenerator.New(), flow, version, now)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	return version, s.repository.ActivateFlowVersionWithResult(ctx, flow, previousVersion, version, result, event)
}

func (s *Service) buildFlowVersion(input CreateFlowVersionInput, now time.Time) (entity.FlowVersion, error) {
	version := entity.FlowVersion{
		ID:               s.idGenerator.New(),
		FlowID:           input.FlowID,
		Version:          0,
		SourceRef:        strings.TrimSpace(input.SourceRef),
		DefinitionDigest: strings.TrimSpace(input.DefinitionDigest),
		Status:           enum.FlowVersionStatusDraft,
		CreatedAt:        now,
	}
	stageIDs := make(map[string]uuid.UUID, len(input.Stages))
	for _, candidate := range input.Stages {
		if err := validateSlug(candidate.Slug); err != nil {
			return entity.FlowVersion{}, err
		}
		if _, exists := stageIDs[candidate.Slug]; exists {
			return entity.FlowVersion{}, errs.ErrInvalidArgument
		}
		requiredArtifacts, err := normalizeObjectPayload(candidate.RequiredArtifactsJSON)
		if err != nil {
			return entity.FlowVersion{}, err
		}
		acceptancePolicy, err := normalizeObjectPayload(candidate.AcceptancePolicyJSON)
		if err != nil {
			return entity.FlowVersion{}, err
		}
		stage := entity.Stage{
			ID:                    s.idGenerator.New(),
			FlowVersionID:         version.ID,
			Slug:                  strings.TrimSpace(candidate.Slug),
			StageType:             candidate.StageType,
			DisplayName:           candidate.DisplayName,
			IconObjectURI:         strings.TrimSpace(candidate.IconObjectURI),
			RequiredArtifactsJSON: requiredArtifacts,
			AcceptancePolicyJSON:  acceptancePolicy,
			Position:              candidate.Position,
		}
		stageIDs[stage.Slug] = stage.ID
		version.Stages = append(version.Stages, stage)
	}
	for position, candidate := range input.Transitions {
		toStageID, exists := stageIDs[candidate.ToStageSlug]
		if !exists {
			return entity.FlowVersion{}, errs.ErrPreconditionFailed
		}
		var fromStageID *uuid.UUID
		if candidate.FromStageSlug != nil && strings.TrimSpace(*candidate.FromStageSlug) != "" {
			stageID, exists := stageIDs[strings.TrimSpace(*candidate.FromStageSlug)]
			if !exists {
				return entity.FlowVersion{}, errs.ErrPreconditionFailed
			}
			fromStageID = &stageID
		}
		condition, err := normalizeObjectPayload(candidate.ConditionJSON)
		if err != nil {
			return entity.FlowVersion{}, err
		}
		version.Transitions = append(version.Transitions, entity.StageTransition{
			ID:            s.idGenerator.New(),
			FlowVersionID: version.ID,
			FromStageID:   fromStageID,
			ToStageID:     toStageID,
			ConditionJSON: condition,
			FollowUpType:  strings.TrimSpace(candidate.FollowUpType),
			Position:      int32(position),
		})
	}
	for _, candidate := range input.RoleBindings {
		stageID, exists := stageIDs[candidate.StageSlug]
		if !exists || candidate.RoleProfileID == uuid.Nil {
			return entity.FlowVersion{}, errs.ErrPreconditionFailed
		}
		launchPolicy, err := normalizeObjectPayload(candidate.LaunchPolicyJSON)
		if err != nil {
			return entity.FlowVersion{}, err
		}
		version.RoleBindings = append(version.RoleBindings, entity.StageRoleBinding{
			ID:                    s.idGenerator.New(),
			StageID:               stageID,
			RoleProfileID:         candidate.RoleProfileID,
			BindingKind:           candidate.BindingKind,
			LaunchPolicyJSON:      launchPolicy,
			RequiredForAcceptance: candidate.RequiredForAcceptance,
		})
	}
	return version, nil
}

func (s *Service) nextFlowVersion(ctx context.Context, flowID uuid.UUID) (int64, error) {
	versions, _, err := s.repository.ListFlowVersions(ctx, query.FlowVersionFilter{
		FlowID: flowID,
		Page:   value.PageRequest{PageSize: 1},
	})
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 1, nil
	}
	return versions[0].Version + 1, nil
}

func flowFromPayload(payload []byte) (entity.Flow, error) {
	var result flowCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.Flow, err
}

func flowVersionFromPayload(payload []byte) (entity.FlowVersion, error) {
	var result flowVersionCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.FlowVersion, err
}
