package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
)

type bootstrapServicesPolicyImportCommandPayload struct {
	SourceRef                    string `json:"source_ref,omitempty"`
	SourceCommitSHA              string `json:"source_commit_sha,omitempty"`
	ContentHash                  string `json:"content_hash,omitempty"`
	ProviderWorkItemProjectionID string `json:"provider_work_item_projection_id,omitempty"`
	ProviderWebURL               string `json:"provider_web_url,omitempty"`
	ProviderObjectID             string `json:"provider_object_id,omitempty"`
	Summary                      string `json:"summary,omitempty"`
}

type normalizedBootstrapPolicyImport struct {
	ProviderSlug                 string
	ProviderTarget               RepositoryBootstrapProviderTarget
	BaseBranch                   string
	SourceRef                    string
	SourceCommitSHA              string
	SourceBlobSHA                string
	SourcePath                   string
	ContentHash                  string
	ValidatedPayload             []byte
	ProviderWorkItemProjectionID string
	ProviderWebURL               string
	ProviderObjectID             string
	Summary                      string
}

// ImportBootstrapServicesPolicy imports checked services.yaml after bootstrap PR merge and activates the binding.
func (s *Service) ImportBootstrapServicesPolicy(ctx context.Context, input ImportBootstrapServicesPolicyInput) (BootstrapServicesPolicyImportResult, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	if input.RepositoryID == uuid.Nil {
		return BootstrapServicesPolicyImportResult{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionPolicyImport, projectScopedResource(projectAggregateServicesPolicy, input.ProjectID)); err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	if replay, ok, err := s.replayBootstrapServicesPolicyImport(ctx, input); ok || err != nil {
		return replay, err
	}
	repository, err := s.repository.GetRepository(ctx, input.RepositoryID)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	if repository.ProjectID != input.ProjectID {
		return BootstrapServicesPolicyImportResult{}, errs.ErrPreconditionFailed
	}
	normalized, err := normalizeBootstrapPolicyImportInput(input, repository)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	if replay, ok, err := s.replayBootstrapServicesPolicyImportBySource(ctx, repository, normalized); ok || err != nil {
		return replay, err
	}
	if repository.Status != enum.RepositoryStatusPending {
		return BootstrapServicesPolicyImportResult{}, errs.ErrPreconditionFailed
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	now := s.clock.Now()
	projection, err := buildServicesPolicyProjection(ImportServicesPolicyInput{
		ProjectID:          input.ProjectID,
		SourceRepositoryID: &input.RepositoryID,
		ValidatedPayload:   normalized.ValidatedPayload,
	}, enum.ServicesPolicyValidationValid)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	policy := entity.ServicesPolicy{
		Base:               newBase(s.ids.New(), now),
		ProjectID:          input.ProjectID,
		SourceRepositoryID: &input.RepositoryID,
		SourcePath:         normalized.SourcePath,
		SourceRef:          normalized.SourceRef,
		SourceCommitSHA:    normalized.SourceCommitSHA,
		SourceBlobSHA:      normalized.SourceBlobSHA,
		ContentHash:        normalized.ContentHash,
		ValidatedPayload:   projection.payload,
		ValidationStatus:   enum.ServicesPolicyValidationValid,
		ProjectionStatus:   enum.ServicesPolicyProjectionSynced,
		ImportedAt:         now,
	}
	updatedRepository := repository
	updatedRepository.Base = updatedBase(repository.Base, now)
	updatedRepository.Status = enum.RepositoryStatusActive
	descriptors := s.prepareServiceDescriptors(policy, projection.descriptors, now)
	documentationSources := s.preparePolicyDocumentationSources(policy, projection.documentationSources, now)
	payload, err := bootstrapPolicyImportCommandPayloadJSON(normalized)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	command, err := commandResultWithPayload(input.Meta, projectOperationImportBootstrapPolicy, projectAggregateServicesPolicy, policy.ID, now, payload)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	repositoryEvent, err := s.repositoryEvent(projectEventRepositoryUpdated, updatedRepository)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	buildPolicyEvent := func(policy entity.ServicesPolicy) (entity.OutboxEvent, error) {
		return s.bootstrapServicesPolicyImportedEvent(policy, updatedRepository, normalized)
	}
	imported, activated, err := s.repository.ImportBootstrapServicesPolicy(
		ctx,
		updatedRepository,
		previousVersion,
		policy,
		descriptors,
		documentationSources,
		repositoryEvent,
		*command,
		buildPolicyEvent,
	)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	return bootstrapServicesPolicyImportResult(activated, imported, normalized), nil
}

func (s *Service) replayBootstrapServicesPolicyImport(ctx context.Context, input ImportBootstrapServicesPolicyInput) (BootstrapServicesPolicyImportResult, bool, error) {
	result, ok, err := s.findCommandResult(ctx, input.Meta, projectOperationImportBootstrapPolicy, projectAggregateServicesPolicy)
	if err != nil || !ok {
		return BootstrapServicesPolicyImportResult{}, ok, err
	}
	policy, err := s.repository.GetServicesPolicy(ctx, input.ProjectID, &result.AggregateID)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, true, err
	}
	repository, err := s.repository.GetRepository(ctx, input.RepositoryID)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, true, err
	}
	if policy.ProjectID != input.ProjectID || policy.SourceRepositoryID == nil || *policy.SourceRepositoryID != input.RepositoryID || repository.ProjectID != input.ProjectID {
		return BootstrapServicesPolicyImportResult{}, true, errs.ErrConflict
	}
	payload := decodeBootstrapPolicyImportCommandPayload(result.ResultPayload, policy)
	return BootstrapServicesPolicyImportResult{
		Repository:      repository,
		ServicesPolicy:  policy,
		SourceRef:       firstNonEmpty(payload.SourceRef, policy.SourceRef),
		SourceCommitSHA: policy.SourceCommitSHA,
		Summary:         firstNonEmpty(payload.Summary, servicesPolicyImportSummary(policy.SourceRef, policy.SourceCommitSHA)),
	}, true, nil
}

func (s *Service) replayBootstrapServicesPolicyImportBySource(ctx context.Context, repository entity.RepositoryBinding, input normalizedBootstrapPolicyImport) (BootstrapServicesPolicyImportResult, bool, error) {
	policy, err := s.repository.GetServicesPolicyBySource(ctx, repository.ProjectID, repository.ID, input.SourcePath, input.SourceCommitSHA)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			if repository.Status == enum.RepositoryStatusActive {
				return BootstrapServicesPolicyImportResult{}, true, errs.ErrConflict
			}
			return BootstrapServicesPolicyImportResult{}, false, nil
		}
		return BootstrapServicesPolicyImportResult{}, false, err
	}
	if policy.ContentHash != input.ContentHash || policy.SourceRef != input.SourceRef {
		return BootstrapServicesPolicyImportResult{}, true, errs.ErrConflict
	}
	if repository.Status != enum.RepositoryStatusActive {
		return BootstrapServicesPolicyImportResult{}, true, errs.ErrPreconditionFailed
	}
	return bootstrapServicesPolicyImportResult(repository, policy, input), true, nil
}

func normalizeBootstrapPolicyImportInput(input ImportBootstrapServicesPolicyInput, repository entity.RepositoryBinding) (normalizedBootstrapPolicyImport, error) {
	if err := validateBootstrapRepository(repository); err != nil {
		return normalizedBootstrapPolicyImport{}, err
	}
	providerSlug, err := repositoryProviderSlug(repository.Provider)
	if err != nil {
		return normalizedBootstrapPolicyImport{}, err
	}
	sourcePath := strings.TrimSpace(input.SourcePath)
	sourceRef := strings.TrimSpace(input.SourceRef)
	sourceCommitSHA := strings.ToLower(strings.TrimSpace(input.SourceCommitSHA))
	sourceBlobSHA := strings.TrimSpace(input.SourceBlobSHA)
	baseBranch := strings.TrimSpace(input.BaseBranch)
	contentHash := strings.ToLower(strings.TrimSpace(input.ContentHash))
	payload := []byte(strings.TrimSpace(string(input.ValidatedPayload)))
	providerWebURL, err := normalizeSafeProviderURL(input.ProviderWebURL)
	if err != nil {
		return normalizedBootstrapPolicyImport{}, err
	}
	if err := validateBootstrapPolicyProviderTarget(providerSlug, repository, input.ProviderTarget); err != nil {
		return normalizedBootstrapPolicyImport{}, err
	}
	if baseBranch == "" || baseBranch != repository.DefaultBranch || !sourceRefMatchesBaseBranch(sourceRef, baseBranch) {
		return normalizedBootstrapPolicyImport{}, errs.ErrInvalidArgument
	}
	if sourcePath != "services.yaml" || !validBootstrapFilePath(sourcePath) || !validGitCommitSHA(sourceCommitSHA) || !validSHA256ContentHash(contentHash) {
		return normalizedBootstrapPolicyImport{}, errs.ErrInvalidArgument
	}
	if len(payload) == 0 || !json.Valid(payload) {
		return normalizedBootstrapPolicyImport{}, errs.ErrInvalidArgument
	}
	watermark, err := parseBootstrapPolicyImportWatermark(input.WatermarkJSON)
	if err != nil {
		return normalizedBootstrapPolicyImport{}, err
	}
	if watermark.SourceRef != sourcePath {
		return normalizedBootstrapPolicyImport{}, errs.ErrInvalidArgument
	}
	if strings.TrimSpace(input.MergeObservedAt) != "" {
		if _, err := parseRFC3339(input.MergeObservedAt); err != nil {
			return normalizedBootstrapPolicyImport{}, err
		}
	}
	summary := servicesPolicyImportSummary(sourceRef, sourceCommitSHA)
	return normalizedBootstrapPolicyImport{
		ProviderSlug:                 providerSlug,
		ProviderTarget:               normalizeBootstrapPolicyProviderTarget(input.ProviderTarget),
		BaseBranch:                   baseBranch,
		SourceRef:                    sourceRef,
		SourceCommitSHA:              sourceCommitSHA,
		SourceBlobSHA:                sourceBlobSHA,
		SourcePath:                   sourcePath,
		ContentHash:                  contentHash,
		ValidatedPayload:             payload,
		ProviderWorkItemProjectionID: strings.TrimSpace(input.ProviderWorkItemProjectionID),
		ProviderWebURL:               providerWebURL,
		ProviderObjectID:             strings.TrimSpace(input.ProviderObjectID),
		Summary:                      summary,
	}, nil
}

func validateBootstrapPolicyProviderTarget(providerSlug string, repository entity.RepositoryBinding, target RepositoryBootstrapProviderTarget) error {
	normalized := normalizeBootstrapPolicyProviderTarget(target)
	expectedFullName := strings.TrimSpace(repository.ProviderOwner) + "/" + strings.TrimSpace(repository.ProviderName)
	if normalized.ProviderSlug != providerSlug || normalized.RepositoryFullName != expectedFullName {
		return errs.ErrPreconditionFailed
	}
	if normalized.ProviderRepositoryID != "" && repository.ProviderRepositoryID != "" && normalized.ProviderRepositoryID != repository.ProviderRepositoryID {
		return errs.ErrPreconditionFailed
	}
	if normalized.WebURL != "" && repository.WebURL != "" && normalized.WebURL != repository.WebURL {
		return errs.ErrPreconditionFailed
	}
	if _, err := normalizeSafeProviderURL(normalized.WebURL); err != nil {
		return err
	}
	return nil
}

func normalizeBootstrapPolicyProviderTarget(target RepositoryBootstrapProviderTarget) RepositoryBootstrapProviderTarget {
	return RepositoryBootstrapProviderTarget{
		ProviderSlug:         strings.TrimSpace(target.ProviderSlug),
		RepositoryFullName:   strings.TrimSpace(target.RepositoryFullName),
		ProviderRepositoryID: strings.TrimSpace(target.ProviderRepositoryID),
		WebURL:               strings.TrimSpace(target.WebURL),
	}
}

func parseBootstrapPolicyImportWatermark(raw []byte) (bootstrapWatermark, error) {
	payload, err := normalizeBootstrapWatermark(raw)
	if err != nil {
		return bootstrapWatermark{}, err
	}
	var watermark bootstrapWatermark
	if err := json.Unmarshal(payload, &watermark); err != nil {
		return bootstrapWatermark{}, errs.ErrInvalidArgument
	}
	if strings.TrimSpace(watermark.Kind) != "provider_pr" ||
		strings.TrimSpace(watermark.ManagedBy) != "kodex" ||
		strings.TrimSpace(watermark.WorkType) != "repository_bootstrap" {
		return bootstrapWatermark{}, errs.ErrInvalidArgument
	}
	watermark.SourceRef = strings.TrimSpace(watermark.SourceRef)
	return watermark, nil
}

func sourceRefMatchesBaseBranch(sourceRef string, baseBranch string) bool {
	return sourceRef == baseBranch || sourceRef == "refs/heads/"+baseBranch
}

func validGitCommitSHA(text string) bool {
	if len(text) != 40 && len(text) != 64 {
		return false
	}
	for _, char := range text {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F') {
			continue
		}
		return false
	}
	return true
}

func validSHA256ContentHash(text string) bool {
	hash := strings.TrimSpace(strings.ToLower(text))
	const prefix = "sha256:"
	if !strings.HasPrefix(hash, prefix) {
		return false
	}
	encoded := strings.TrimPrefix(hash, prefix)
	if len(encoded) != 64 {
		return false
	}
	for _, char := range encoded {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}

func normalizeSafeProviderURL(text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return "", errs.ErrInvalidArgument
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", errs.ErrInvalidArgument
	}
	return trimmed, nil
}

func bootstrapPolicyImportCommandPayloadJSON(input normalizedBootstrapPolicyImport) ([]byte, error) {
	payload, err := json.Marshal(bootstrapServicesPolicyImportCommandPayload{
		SourceRef:                    input.SourceRef,
		SourceCommitSHA:              input.SourceCommitSHA,
		ContentHash:                  input.ContentHash,
		ProviderWorkItemProjectionID: input.ProviderWorkItemProjectionID,
		ProviderWebURL:               input.ProviderWebURL,
		ProviderObjectID:             input.ProviderObjectID,
		Summary:                      input.Summary,
	})
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func decodeBootstrapPolicyImportCommandPayload(payload []byte, policy entity.ServicesPolicy) bootstrapServicesPolicyImportCommandPayload {
	var stored bootstrapServicesPolicyImportCommandPayload
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &stored)
	}
	if stored.SourceRef == "" {
		stored.SourceRef = policy.SourceRef
	}
	if stored.SourceCommitSHA == "" {
		stored.SourceCommitSHA = policy.SourceCommitSHA
	}
	if stored.ContentHash == "" {
		stored.ContentHash = policy.ContentHash
	}
	return stored
}

func (s *Service) bootstrapServicesPolicyImportedEvent(policy entity.ServicesPolicy, repository entity.RepositoryBinding, input normalizedBootstrapPolicyImport) (entity.OutboxEvent, error) {
	options := []projectEventPayloadOption{
		payloadProjectID(policy.ProjectID),
		payloadRepositoryID(repository.ID),
		payloadField(projectPayloadPolicyID, policy.ID.String()),
		payloadPolicyVersion(policy.PolicyVersion),
		payloadField(projectPayloadSourcePath, policy.SourcePath),
		payloadField(projectPayloadSourceRef, policy.SourceRef),
		payloadField(projectPayloadSourceCommit, policy.SourceCommitSHA),
		payloadField(projectPayloadContentHash, policy.ContentHash),
		payloadField(projectPayloadSummary, input.Summary),
	}
	if policy.SourceBlobSHA != "" {
		options = append(options, payloadField(projectPayloadSourceBlob, policy.SourceBlobSHA))
	}
	if input.ProviderWorkItemProjectionID != "" {
		options = append(options, payloadField(projectPayloadProviderWorkItemProjectionID, input.ProviderWorkItemProjectionID))
	}
	if input.ProviderWebURL != "" {
		options = append(options, payloadField(projectPayloadProviderWebURL, input.ProviderWebURL))
	}
	return s.aggregateEvent(projectEventServicesPolicyImported, projectAggregateServicesPolicy, policy.ID, policy.ImportedAt, options...)
}

func bootstrapServicesPolicyImportResult(repository entity.RepositoryBinding, policy entity.ServicesPolicy, input normalizedBootstrapPolicyImport) BootstrapServicesPolicyImportResult {
	return BootstrapServicesPolicyImportResult{
		Repository:      repository,
		ServicesPolicy:  policy,
		SourceRef:       input.SourceRef,
		SourceCommitSHA: policy.SourceCommitSHA,
		Summary:         firstNonEmpty(input.Summary, servicesPolicyImportSummary(policy.SourceRef, policy.SourceCommitSHA)),
	}
}

func shortCommitSHA(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= 12 {
		return trimmed
	}
	return trimmed[:12]
}
