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

type promptTemplateCommandPayload struct {
	PromptTemplate entity.PromptTemplate `json:"prompt_template"`
}

type promptTemplateVersionCommandPayload struct {
	PromptTemplateVersion entity.PromptTemplateVersion `json:"prompt_template_version"`
}

func (s *Service) CreatePromptTemplate(ctx context.Context, input CreatePromptTemplateInput) (entity.PromptTemplate, error) {
	if err := s.requireRepository(); err != nil {
		return entity.PromptTemplate{}, err
	}
	if err := validateID(input.RoleProfileID); err != nil {
		return entity.PromptTemplate{}, err
	}
	if input.PromptKind == "" {
		return entity.PromptTemplate{}, errs.ErrInvalidArgument
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationCreatePromptTemplate, enum.CommandAggregateTypePromptTemplate, promptTemplateFromPayload, verifyPromptReplay(uuid.Nil, input.RoleProfileID, input.PromptKind, s.repository.GetPromptTemplate, promptTemplateID, promptTemplateRoleID, promptTemplateKind)); ok || err != nil {
		return replay, err
	}
	now := s.clock.Now()
	template := entity.PromptTemplate{
		VersionedBase: entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		RoleProfileID: input.RoleProfileID,
		PromptKind:    input.PromptKind,
	}
	payload, err := marshalCommandPayload(promptTemplateCommandPayload{PromptTemplate: template})
	if err != nil {
		return entity.PromptTemplate{}, err
	}
	result, err := commandResult(input.Meta, operationCreatePromptTemplate, enum.CommandAggregateTypePromptTemplate, template.ID, payload, now)
	if err != nil {
		return entity.PromptTemplate{}, err
	}
	return template, s.repository.CreatePromptTemplateWithResult(ctx, template, result)
}

func (s *Service) GetPromptTemplate(ctx context.Context, id uuid.UUID) (entity.PromptTemplate, error) {
	return getByID(ctx, s, id, s.getPromptTemplateFromRepository)
}

func (s *Service) ListPromptTemplates(ctx context.Context, filter query.PromptTemplateFilter) ([]entity.PromptTemplate, value.PageResult, error) {
	return listFromRepository(ctx, s, filter, s.listPromptTemplatesFromRepository)
}

func (s *Service) CreatePromptTemplateVersion(ctx context.Context, input CreatePromptTemplateVersionInput) (entity.PromptTemplateVersion, error) {
	if err := s.requireRepository(); err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	if err := validateID(input.RoleProfileID); err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	if input.PromptKind == "" || strings.TrimSpace(input.TemplateDigest) == "" {
		return entity.PromptTemplateVersion{}, errs.ErrInvalidArgument
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationCreatePromptTemplateVersion, enum.CommandAggregateTypePromptTemplateVersion, promptTemplateVersionFromPayload, verifyPromptReplay(uuid.Nil, input.RoleProfileID, input.PromptKind, s.repository.GetPromptTemplateVersion, promptVersionID, promptVersionRoleID, promptVersionKind)); ok || err != nil {
		return replay, err
	}
	now := s.clock.Now()
	template, newTemplate, err := s.resolvePromptTemplate(ctx, input.RoleProfileID, input.PromptKind, now)
	if err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	nextVersion, err := s.nextPromptTemplateVersion(ctx, input.RoleProfileID, input.PromptKind)
	if err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	version := entity.PromptTemplateVersion{
		ID:               s.idGenerator.New(),
		PromptTemplateID: template.ID,
		RoleProfileID:    input.RoleProfileID,
		PromptKind:       input.PromptKind,
		Version:          nextVersion,
		SourceRef:        strings.TrimSpace(input.SourceRef),
		TemplateObject:   input.TemplateObject,
		TemplateDigest:   strings.TrimSpace(input.TemplateDigest),
		Status:           enum.PromptVersionStatusDraft,
		CreatedAt:        now,
	}
	payload, err := marshalCommandPayload(promptTemplateVersionCommandPayload{PromptTemplateVersion: version})
	if err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	result, err := commandResult(input.Meta, operationCreatePromptTemplateVersion, enum.CommandAggregateTypePromptTemplateVersion, version.ID, payload, now)
	if err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	return s.repository.CreatePromptTemplateVersionWithResult(ctx, newTemplate, version, result)
}

func (s *Service) ActivatePromptTemplateVersion(ctx context.Context, input ActivatePromptTemplateVersionInput) (entity.PromptTemplateVersion, error) {
	if err := s.requireRepository(); err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	if err := validateID(input.PromptTemplateVersionID); err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationActivatePromptVersion, enum.CommandAggregateTypePromptTemplateVersion, promptTemplateVersionFromPayload, verifyPromptReplay(input.PromptTemplateVersionID, uuid.Nil, "", s.repository.GetPromptTemplateVersion, promptVersionID, promptVersionRoleID, promptVersionKind)); ok || err != nil {
		return replay, err
	}
	version, err := s.repository.GetPromptTemplateVersion(ctx, input.PromptTemplateVersionID)
	if err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	template, err := s.repository.GetPromptTemplate(ctx, version.PromptTemplateID)
	if err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	if template.Version != previousVersion {
		return entity.PromptTemplateVersion{}, errs.ErrConflict
	}
	now := s.clock.Now()
	version.Status = enum.PromptVersionStatusActive
	version.ActivatedAt = &now
	template.ActiveVersionID = &version.ID
	template.Version++
	template.UpdatedAt = now
	payload, err := marshalCommandPayload(promptTemplateVersionCommandPayload{PromptTemplateVersion: version})
	if err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	result, err := commandResult(input.Meta, operationActivatePromptVersion, enum.CommandAggregateTypePromptTemplateVersion, version.ID, payload, now)
	if err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	event, err := promptActivatedEvent(s.idGenerator.New(), template, version, now)
	if err != nil {
		return entity.PromptTemplateVersion{}, err
	}
	return version, s.repository.ActivatePromptTemplateVersionWithResult(ctx, template, previousVersion, version, result, event)
}

func (s *Service) GetPromptTemplateVersion(ctx context.Context, id uuid.UUID) (entity.PromptTemplateVersion, error) {
	return getByID(ctx, s, id, s.getPromptTemplateVersionFromRepository)
}

func (s *Service) ListPromptTemplateVersions(ctx context.Context, filter query.PromptTemplateVersionFilter) ([]entity.PromptTemplateVersion, value.PageResult, error) {
	return listFromRepository(ctx, s, filter, s.listPromptVersionsFromRepository)
}

func (s *Service) getPromptTemplateFromRepository(ctx context.Context, id uuid.UUID) (entity.PromptTemplate, error) {
	return s.repository.GetPromptTemplate(ctx, id)
}

func (s *Service) listPromptTemplatesFromRepository(ctx context.Context, filter query.PromptTemplateFilter) ([]entity.PromptTemplate, value.PageResult, error) {
	return s.repository.ListPromptTemplates(ctx, filter)
}

func (s *Service) getPromptTemplateVersionFromRepository(ctx context.Context, id uuid.UUID) (entity.PromptTemplateVersion, error) {
	return s.repository.GetPromptTemplateVersion(ctx, id)
}

func (s *Service) listPromptVersionsFromRepository(ctx context.Context, filter query.PromptTemplateVersionFilter) ([]entity.PromptTemplateVersion, value.PageResult, error) {
	return s.repository.ListPromptTemplateVersions(ctx, filter)
}

func promptTemplateFromPayload(payload []byte) (entity.PromptTemplate, error) {
	var result promptTemplateCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.PromptTemplate, err
}

func (s *Service) resolvePromptTemplate(ctx context.Context, roleProfileID uuid.UUID, kind enum.PromptKind, now time.Time) (entity.PromptTemplate, *entity.PromptTemplate, error) {
	kindFilter := kind
	templates, _, err := s.repository.ListPromptTemplates(ctx, query.PromptTemplateFilter{
		RoleProfileID: roleProfileID,
		Kind:          &kindFilter,
		Page:          value.PageRequest{PageSize: 1},
	})
	if err != nil {
		return entity.PromptTemplate{}, nil, err
	}
	if len(templates) > 0 {
		return templates[0], nil, nil
	}
	template := entity.PromptTemplate{
		VersionedBase: entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		RoleProfileID: roleProfileID,
		PromptKind:    kind,
	}
	return template, &template, nil
}

func (s *Service) nextPromptTemplateVersion(ctx context.Context, roleProfileID uuid.UUID, kind enum.PromptKind) (int64, error) {
	kindFilter := kind
	versions, _, err := s.repository.ListPromptTemplateVersions(ctx, query.PromptTemplateVersionFilter{
		RoleProfileID: roleProfileID,
		Kind:          &kindFilter,
		Page:          value.PageRequest{PageSize: 1},
	})
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 1, nil
	}
	return versions[0].Version + 1, nil
}

func promptTemplateVersionFromPayload(payload []byte) (entity.PromptTemplateVersion, error) {
	var result promptTemplateVersionCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.PromptTemplateVersion, err
}
