package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

type providerRepositoryCommandPayload struct {
	ProviderOperationID  string `json:"provider_operation_id,omitempty"`
	ProviderResultRef    string `json:"provider_result_ref,omitempty"`
	ProviderRepositoryID string `json:"provider_repository_id,omitempty"`
	ProviderWebURL       string `json:"provider_web_url,omitempty"`
	ProviderObjectID     string `json:"provider_object_id,omitempty"`
	ProviderVersion      string `json:"provider_version,omitempty"`
	BaseBranch           string `json:"base_branch,omitempty"`
	RepositoryFullName   string `json:"repository_full_name,omitempty"`
}

// CreateProviderRepository creates a provider-native repository and records the project binding.
func (s *Service) CreateProviderRepository(ctx context.Context, input CreateProviderRepositoryInput) (RepositoryProviderCreateResult, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	if input.ExternalAccountID == uuid.Nil {
		return RepositoryProviderCreateResult{}, errs.ErrInvalidArgument
	}
	providerSlug, err := repositoryProviderSlug(input.Provider)
	if err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	ownerKind, err := normalizeRepositoryOwnerKind(input.OwnerKind)
	if err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	visibility, err := normalizeRepositoryVisibility(input.Visibility)
	if err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	providerOwner := strings.TrimSpace(input.ProviderOwner)
	providerName := strings.TrimSpace(input.ProviderName)
	if !validProviderOwnerRef(providerOwner) || !validProviderRepositoryName(providerName) {
		return RepositoryProviderCreateResult{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionRepositoryAttach, projectScopedResource(projectAggregateRepository, input.ProjectID)); err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	if replay, ok, err := s.replayProviderRepositoryCreate(ctx, input.ProjectID, input.Meta); ok || err != nil {
		return replay, err
	}
	if s.bootstrapProvider == nil {
		return RepositoryProviderCreateResult{}, errs.ErrDependencyUnavailable
	}
	binding, err := s.pendingProviderRepositoryBinding(ctx, input, providerOwner, providerName)
	if err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	providerOwnerForWrite := providerOwner
	if ownerKind == enum.RepositoryOwnerKindAuthenticatedUser {
		providerOwnerForWrite = ""
	}
	providerResult, err := s.bootstrapProvider.CreateProviderRepository(ctx, ProviderRepositoryCreateInput{
		ProjectID:         input.ProjectID,
		RepositoryID:      binding.ID,
		ProviderSlug:      providerSlug,
		OwnerKind:         ownerKind,
		ProviderOwner:     providerOwnerForWrite,
		RepositoryName:    providerName,
		Visibility:        visibility,
		Description:       strings.TrimSpace(input.Description),
		ExternalAccountID: input.ExternalAccountID,
		Meta:              input.Meta,
	})
	if err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	return s.completeProviderRepositoryBinding(ctx, binding, providerSlug, providerResult, input.Meta)
}

func (s *Service) replayProviderRepositoryCreate(ctx context.Context, projectID uuid.UUID, meta value.CommandMeta) (RepositoryProviderCreateResult, bool, error) {
	result, ok, err := s.findCommandResult(ctx, meta, projectOperationCreateProviderRepo, projectAggregateRepository)
	if err != nil || !ok {
		return RepositoryProviderCreateResult{}, ok, err
	}
	repository, err := s.repository.GetRepository(ctx, result.AggregateID)
	if err != nil {
		return RepositoryProviderCreateResult{}, true, err
	}
	if repository.ProjectID != projectID {
		return RepositoryProviderCreateResult{}, true, errs.ErrConflict
	}
	providerSlug, err := repositoryProviderSlug(repository.Provider)
	if err != nil {
		return RepositoryProviderCreateResult{}, true, err
	}
	providerResult := decodeProviderRepositoryCommandPayload(result.ResultPayload, repository)
	return providerRepositoryCreateResult(repository, providerSlug, providerResult), true, nil
}

func (s *Service) pendingProviderRepositoryBinding(ctx context.Context, input CreateProviderRepositoryInput, providerOwner string, providerName string) (entity.RepositoryBinding, error) {
	existing, err := s.repository.GetRepositoryByProviderRef(ctx, input.Provider, providerOwner, providerName)
	if err == nil {
		return reusablePendingProviderRepositoryBinding(existing, input.ProjectID)
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return entity.RepositoryBinding{}, err
	}
	now := s.clock.Now()
	binding := entity.RepositoryBinding{
		Base:          newBase(s.ids.New(), now),
		ProjectID:     input.ProjectID,
		Provider:      input.Provider,
		ProviderOwner: providerOwner,
		ProviderName:  providerName,
		Status:        enum.RepositoryStatusPending,
		IconObjectURI: strings.TrimSpace(input.IconObjectURI),
	}
	event, err := s.repositoryEvent(projectEventRepositoryAttached, binding)
	if err != nil {
		return entity.RepositoryBinding{}, err
	}
	if err := s.repository.ReserveRepositoryBinding(ctx, binding, event); err != nil {
		if !errors.Is(err, errs.ErrConflict) {
			return entity.RepositoryBinding{}, err
		}
		existing, getErr := s.repository.GetRepositoryByProviderRef(ctx, input.Provider, providerOwner, providerName)
		if getErr != nil {
			return entity.RepositoryBinding{}, err
		}
		return reusablePendingProviderRepositoryBinding(existing, input.ProjectID)
	}
	return binding, nil
}

func reusablePendingProviderRepositoryBinding(binding entity.RepositoryBinding, projectID uuid.UUID) (entity.RepositoryBinding, error) {
	if binding.ProjectID != projectID {
		return entity.RepositoryBinding{}, errs.ErrConflict
	}
	if binding.Status != enum.RepositoryStatusPending ||
		binding.DefaultBranch != "" ||
		binding.ProviderRepositoryID != "" ||
		binding.WebURL != "" {
		return entity.RepositoryBinding{}, errs.ErrConflict
	}
	return binding, nil
}

func (s *Service) completeProviderRepositoryBinding(
	ctx context.Context,
	binding entity.RepositoryBinding,
	providerSlug string,
	providerResult RepositoryProviderCreateProviderResult,
	meta value.CommandMeta,
) (RepositoryProviderCreateResult, error) {
	providerResult = normalizeProviderRepositoryCreateResult(providerResult, binding)
	if providerResult.BaseBranch == "" {
		return RepositoryProviderCreateResult{}, errs.ErrDependencyUnavailable
	}
	now := s.clock.Now()
	updated := binding
	updated.Base = updatedBase(binding.Base, now)
	updated.ProviderOwner, updated.ProviderName = providerOwnerNameFromFullName(providerResult.RepositoryFullName, binding.ProviderOwner, binding.ProviderName)
	updated.WebURL = providerResult.ProviderWebURL
	updated.DefaultBranch = providerResult.BaseBranch
	updated.ProviderRepositoryID = providerResult.ProviderRepositoryID
	updated.Status = enum.RepositoryStatusPending

	payload, err := encodeProviderRepositoryCommandPayload(providerResult)
	if err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	command, err := commandResultWithPayload(meta, projectOperationCreateProviderRepo, projectAggregateRepository, updated.ID, now, payload)
	if err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	event, err := s.repositoryEvent(projectEventRepositoryUpdated, updated)
	if err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	if err := s.repository.UpdateRepository(ctx, updated, binding.Version, event, command); err != nil {
		return RepositoryProviderCreateResult{}, err
	}
	return providerRepositoryCreateResult(updated, providerSlug, providerResult), nil
}

func providerRepositoryCreateResult(repository entity.RepositoryBinding, providerSlug string, providerResult RepositoryProviderCreateProviderResult) RepositoryProviderCreateResult {
	return RepositoryProviderCreateResult{
		Repository:     repository,
		ProviderTarget: bootstrapProviderTarget(providerSlug, repository),
		BaseBranch:     repository.DefaultBranch,
		ProviderResult: providerResult,
	}
}

func normalizeProviderRepositoryCreateResult(result RepositoryProviderCreateProviderResult, binding entity.RepositoryBinding) RepositoryProviderCreateProviderResult {
	result.ProviderOperationID = strings.TrimSpace(result.ProviderOperationID)
	result.ProviderResultRef = strings.TrimSpace(result.ProviderResultRef)
	result.ProviderRepositoryID = strings.TrimSpace(result.ProviderRepositoryID)
	result.ProviderWebURL = strings.TrimSpace(result.ProviderWebURL)
	result.ProviderObjectID = strings.TrimSpace(result.ProviderObjectID)
	result.ProviderVersion = strings.TrimSpace(result.ProviderVersion)
	result.BaseBranch = strings.TrimSpace(result.BaseBranch)
	result.RepositoryFullName = strings.TrimSpace(result.RepositoryFullName)
	if result.ProviderRepositoryID == "" {
		result.ProviderRepositoryID = result.ProviderObjectID
	}
	if result.RepositoryFullName == "" {
		result.RepositoryFullName = strings.TrimSpace(binding.ProviderOwner) + "/" + strings.TrimSpace(binding.ProviderName)
	}
	return result
}

func encodeProviderRepositoryCommandPayload(result RepositoryProviderCreateProviderResult) ([]byte, error) {
	payload, err := json.Marshal(providerRepositoryCommandPayload(result))
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func decodeProviderRepositoryCommandPayload(payload []byte, repository entity.RepositoryBinding) RepositoryProviderCreateProviderResult {
	var stored providerRepositoryCommandPayload
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &stored)
	}
	return normalizeProviderRepositoryCreateResult(RepositoryProviderCreateProviderResult{
		ProviderOperationID:  stored.ProviderOperationID,
		ProviderResultRef:    stored.ProviderResultRef,
		ProviderRepositoryID: stored.ProviderRepositoryID,
		ProviderWebURL:       stored.ProviderWebURL,
		ProviderObjectID:     stored.ProviderObjectID,
		ProviderVersion:      stored.ProviderVersion,
		BaseBranch:           firstNonEmpty(stored.BaseBranch, repository.DefaultBranch),
		RepositoryFullName:   firstNonEmpty(stored.RepositoryFullName, strings.TrimSpace(repository.ProviderOwner)+"/"+strings.TrimSpace(repository.ProviderName)),
	}, repository)
}

func normalizeRepositoryOwnerKind(kind enum.RepositoryOwnerKind) (enum.RepositoryOwnerKind, error) {
	switch kind {
	case enum.RepositoryOwnerKindOrganization, enum.RepositoryOwnerKindAuthenticatedUser:
		return kind, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func normalizeRepositoryVisibility(visibility enum.RepositoryVisibility) (enum.RepositoryVisibility, error) {
	switch visibility {
	case enum.RepositoryVisibilityPublic, enum.RepositoryVisibilityPrivate, enum.RepositoryVisibilityInternal:
		return visibility, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func validProviderOwnerRef(owner string) bool {
	if owner == "" || strings.HasPrefix(owner, "/") || strings.HasSuffix(owner, "/") {
		return false
	}
	return !strings.Contains(owner, "\\") && !strings.Contains(owner, "//") && !strings.Contains(owner, "\x00")
}

func validProviderRepositoryName(name string) bool {
	if name == "" || strings.ContainsAny(name, " \t\r\n") {
		return false
	}
	return !strings.Contains(name, "/") && !strings.Contains(name, "\\") && !strings.Contains(name, "\x00")
}

func providerOwnerNameFromFullName(fullName string, fallbackOwner string, fallbackName string) (string, string) {
	fullName = strings.TrimSpace(fullName)
	owner, name, ok := strings.Cut(fullName, "/")
	if !ok {
		return strings.TrimSpace(fallbackOwner), strings.TrimSpace(fallbackName)
	}
	lastSlash := strings.LastIndex(fullName, "/")
	if lastSlash > 0 && lastSlash < len(fullName)-1 {
		owner = fullName[:lastSlash]
		name = fullName[lastSlash+1:]
	}
	if validProviderOwnerRef(owner) && validProviderRepositoryName(name) {
		return owner, name
	}
	return strings.TrimSpace(fallbackOwner), strings.TrimSpace(fallbackName)
}
