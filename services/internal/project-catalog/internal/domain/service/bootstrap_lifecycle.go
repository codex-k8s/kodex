package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
)

const (
	maxBootstrapFiles             = 64
	maxBootstrapFileContentBytes  = 1024 * 1024
	maxBootstrapTotalContentBytes = 4 * 1024 * 1024
)

type bootstrapWatermark struct {
	Kind      string `json:"kind"`
	ManagedBy string `json:"managed_by"`
	WorkType  string `json:"work_type"`
	SourceRef string `json:"source_ref"`
}

// CreateRepositoryBootstrapPullRequest prepares project-owned bootstrap context and delegates provider writes.
func (s *Service) CreateRepositoryBootstrapPullRequest(ctx context.Context, input CreateRepositoryBootstrapPullRequestInput) (RepositoryBootstrapPullRequestResult, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return RepositoryBootstrapPullRequestResult{}, err
	}
	if input.RepositoryID == uuid.Nil || input.ExternalAccountID == uuid.Nil {
		return RepositoryBootstrapPullRequestResult{}, errs.ErrInvalidArgument
	}
	repository, err := s.repository.GetRepository(ctx, input.RepositoryID)
	if err != nil {
		return RepositoryBootstrapPullRequestResult{}, err
	}
	if repository.ProjectID != input.ProjectID {
		return RepositoryBootstrapPullRequestResult{}, errs.ErrPreconditionFailed
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionRepositoryBootstrap, repositoryScopedResource(input.RepositoryID, repository.ProjectID)); err != nil {
		return RepositoryBootstrapPullRequestResult{}, err
	}
	if err := validateBootstrapRepository(repository); err != nil {
		return RepositoryBootstrapPullRequestResult{}, err
	}
	baseBranch := strings.TrimSpace(input.BaseBranch)
	bootstrapBranch := strings.TrimSpace(input.BootstrapBranch)
	if baseBranch == "" || bootstrapBranch == "" || baseBranch != repository.DefaultBranch || baseBranch == bootstrapBranch || !validBootstrapBranchName(bootstrapBranch) {
		return RepositoryBootstrapPullRequestResult{}, errs.ErrInvalidArgument
	}
	files, err := normalizeBootstrapFiles(input.Files)
	if err != nil {
		return RepositoryBootstrapPullRequestResult{}, err
	}
	servicesPolicy, err := normalizeBootstrapServicesPolicy(input.ServicesPolicy, repository.ID)
	if err != nil {
		return RepositoryBootstrapPullRequestResult{}, err
	}
	if !bootstrapFilesContain(files, servicesPolicy.SourcePath) {
		return RepositoryBootstrapPullRequestResult{}, errs.ErrInvalidArgument
	}
	watermarkJSON, err := normalizeBootstrapWatermark(input.WatermarkJSON)
	if err != nil {
		return RepositoryBootstrapPullRequestResult{}, err
	}
	if strings.TrimSpace(input.CommitMessage) == "" || strings.TrimSpace(input.Title) == "" {
		return RepositoryBootstrapPullRequestResult{}, errs.ErrInvalidArgument
	}
	if s.bootstrapProvider == nil {
		return RepositoryBootstrapPullRequestResult{}, errs.ErrDependencyUnavailable
	}
	providerSlug, err := repositoryProviderSlug(repository.Provider)
	if err != nil {
		return RepositoryBootstrapPullRequestResult{}, err
	}
	providerTarget := bootstrapProviderTarget(providerSlug, repository)
	providerResult, err := s.bootstrapProvider.CreateRepositoryBootstrapPullRequest(ctx, ProviderBootstrapPullRequestInput{
		ProjectID:         input.ProjectID,
		RepositoryID:      input.RepositoryID,
		ProviderSlug:      providerSlug,
		RepositoryTarget:  providerTarget,
		BaseBranch:        baseBranch,
		BootstrapBranch:   bootstrapBranch,
		CommitMessage:     strings.TrimSpace(input.CommitMessage),
		Title:             strings.TrimSpace(input.Title),
		Body:              strings.TrimSpace(input.Body),
		Draft:             input.Draft,
		Files:             files,
		WatermarkJSON:     watermarkJSON,
		ServicesPolicy:    servicesPolicy,
		ExternalAccountID: input.ExternalAccountID,
		Meta:              input.Meta,
	})
	if err != nil {
		return RepositoryBootstrapPullRequestResult{}, err
	}
	return RepositoryBootstrapPullRequestResult{
		Repository:      repository,
		ProviderTarget:  providerTarget,
		BaseBranch:      baseBranch,
		BootstrapBranch: bootstrapBranch,
		ServicesPolicy:  servicesPolicy,
		ProviderResult:  providerResult,
	}, nil
}

func validateBootstrapRepository(repository entity.RepositoryBinding) error {
	if repository.Status != enum.RepositoryStatusActive && repository.Status != enum.RepositoryStatusPending {
		return errs.ErrPreconditionFailed
	}
	if repository.ProviderOwner == "" || repository.ProviderName == "" || repository.DefaultBranch == "" {
		return errs.ErrPreconditionFailed
	}
	if _, err := repositoryProviderSlug(repository.Provider); err != nil {
		return err
	}
	return nil
}

func repositoryProviderSlug(provider enum.RepositoryProvider) (string, error) {
	switch provider {
	case enum.RepositoryProviderGitHub:
		return "github", nil
	case enum.RepositoryProviderGitLab:
		return "gitlab", nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func bootstrapProviderTarget(providerSlug string, repository entity.RepositoryBinding) RepositoryBootstrapProviderTarget {
	return RepositoryBootstrapProviderTarget{
		ProviderSlug:         providerSlug,
		RepositoryFullName:   strings.TrimSpace(repository.ProviderOwner) + "/" + strings.TrimSpace(repository.ProviderName),
		ProviderRepositoryID: strings.TrimSpace(repository.ProviderRepositoryID),
		WebURL:               strings.TrimSpace(repository.WebURL),
	}
}

func normalizeBootstrapFiles(files []RepositoryBootstrapFile) ([]RepositoryBootstrapFile, error) {
	if len(files) == 0 || len(files) > maxBootstrapFiles {
		return nil, errs.ErrInvalidArgument
	}
	result := make([]RepositoryBootstrapFile, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	totalSize := 0
	for index := range files {
		file, contentSize, err := normalizeBootstrapFile(files[index])
		if err != nil {
			return nil, err
		}
		if _, ok := seen[file.Path]; ok {
			return nil, errs.ErrInvalidArgument
		}
		seen[file.Path] = struct{}{}
		totalSize += contentSize
		if totalSize > maxBootstrapTotalContentBytes {
			return nil, errs.ErrInvalidArgument
		}
		result = append(result, file)
	}
	return result, nil
}

func normalizeBootstrapFile(file RepositoryBootstrapFile) (RepositoryBootstrapFile, int, error) {
	normalized := RepositoryBootstrapFile{
		Path:       strings.TrimSpace(file.Path),
		Content:    file.Content,
		Executable: file.Executable,
	}
	contentSize := len([]byte(normalized.Content))
	if !validBootstrapFilePath(normalized.Path) || contentSize > maxBootstrapFileContentBytes {
		return RepositoryBootstrapFile{}, 0, errs.ErrInvalidArgument
	}
	return normalized, contentSize, nil
}

func validBootstrapFilePath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	for _, marker := range []string{"\\", "//", "\x00"} {
		if strings.Contains(path, marker) {
			return false
		}
	}
	segments := strings.Split(path, "/")
	for index := range segments {
		switch segments[index] {
		case "", ".", "..":
			return false
		default:
			continue
		}
	}
	return len(segments) > 0
}

func validBootstrapBranchName(branch string) bool {
	return branch != "" &&
		!strings.ContainsAny(branch, " \t\r\n") &&
		!strings.HasPrefix(branch, "/") &&
		!strings.HasSuffix(branch, "/") &&
		!strings.Contains(branch, "\\") &&
		!strings.Contains(branch, "..") &&
		!strings.Contains(branch, "\x00")
}

func normalizeBootstrapServicesPolicy(policy RepositoryBootstrapServicesPolicy, repositoryID uuid.UUID) (RepositoryBootstrapServicesPolicy, error) {
	sourcePath := strings.TrimSpace(policy.SourcePath)
	contentHash := strings.TrimSpace(policy.ContentHash)
	payload := []byte(strings.TrimSpace(string(policy.ValidatedPayload)))
	if sourcePath == "" || contentHash == "" || len(payload) == 0 || !validBootstrapFilePath(sourcePath) || !json.Valid(payload) {
		return RepositoryBootstrapServicesPolicy{}, errs.ErrInvalidArgument
	}
	_, err := buildServicesPolicyProjection(ImportServicesPolicyInput{
		SourceRepositoryID: &repositoryID,
		ValidatedPayload:   payload,
	}, enum.ServicesPolicyValidationValid)
	if err != nil {
		return RepositoryBootstrapServicesPolicy{}, err
	}
	return RepositoryBootstrapServicesPolicy{
		SourcePath:       sourcePath,
		ContentHash:      contentHash,
		ValidatedPayload: payload,
	}, nil
}

func bootstrapFilesContain(files []RepositoryBootstrapFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func normalizeBootstrapWatermark(raw []byte) ([]byte, error) {
	payload := []byte(strings.TrimSpace(string(raw)))
	if len(payload) == 0 || !json.Valid(payload) {
		return nil, errs.ErrInvalidArgument
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil || len(object) == 0 {
		return nil, errs.ErrInvalidArgument
	}
	var watermark bootstrapWatermark
	if err := json.Unmarshal(payload, &watermark); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	if strings.TrimSpace(watermark.Kind) == "" ||
		strings.TrimSpace(watermark.ManagedBy) == "" ||
		strings.TrimSpace(watermark.WorkType) == "" ||
		strings.TrimSpace(watermark.SourceRef) == "" {
		return nil, errs.ErrInvalidArgument
	}
	return payload, nil
}
