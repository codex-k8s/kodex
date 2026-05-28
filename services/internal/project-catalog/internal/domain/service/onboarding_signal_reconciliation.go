package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
)

const safeOnboardingErrorSummaryLimit = 160

const (
	maxOnboardingSignalKeyLength          = 256
	maxOnboardingProviderSlugLength       = 64
	maxOnboardingProviderRefLength        = 256
	maxOnboardingRepositoryFullNameLength = 256
	maxOnboardingArtifactRefLength        = 512
	maxOnboardingArtifactVersionLength    = 128
	maxOnboardingErrorCodeLength          = 64
)

// RecordBootstrapMergeSignalDiagnostic stores safe processing state for a bootstrap merge event that cannot import yet.
func (s *Service) RecordBootstrapMergeSignalDiagnostic(ctx context.Context, input BootstrapMergeSignalDiagnosticInput) error {
	return s.recordRepositoryMergeSignalDiagnosticFor(ctx, input.ProjectID, input.RepositoryID, input.MergeSignal, input.SignalFingerprint, input.ErrorCode, input.ErrorSummary, input.Summary, bootstrapMergeDiagnosticConfig)
}

// RecordAdoptionMergeSignalDiagnostic stores safe processing state for an adoption merge event that cannot import yet.
func (s *Service) RecordAdoptionMergeSignalDiagnostic(ctx context.Context, input AdoptionMergeSignalDiagnosticInput) error {
	return s.recordRepositoryMergeSignalDiagnosticFor(ctx, input.ProjectID, input.RepositoryID, input.MergeSignal, input.SignalFingerprint, input.ErrorCode, input.ErrorSummary, input.Summary, adoptionMergeDiagnosticConfig)
}

type repositoryMergeDiagnosticConfig struct {
	ExpectedSignalKind string
	SignalKind         enum.OnboardingSignalKind
	DefaultSummary     string
}

var (
	bootstrapMergeDiagnosticConfig = repositoryMergeDiagnosticConfig{
		ExpectedSignalKind: bootstrapMergeSignalKind,
		SignalKind:         enum.OnboardingSignalKindBootstrapMerge,
		DefaultSummary:     "bootstrap merge signal needs checked artifact input",
	}
	adoptionMergeDiagnosticConfig = repositoryMergeDiagnosticConfig{
		ExpectedSignalKind: adoptionMergeSignalKind,
		SignalKind:         enum.OnboardingSignalKindAdoptionMerge,
		DefaultSummary:     "adoption merge signal needs checked artifact input",
	}
)

func (s *Service) recordRepositoryMergeSignalDiagnosticFor(
	ctx context.Context,
	projectID uuid.UUID,
	repositoryID uuid.UUID,
	mergeSignal BootstrapRepositoryMergeSignal,
	fingerprint string,
	errorCode string,
	errorSummary string,
	summary string,
	config repositoryMergeDiagnosticConfig,
) error {
	signalInput, code, normalizedSummary, err := normalizeRepositoryMergeSignalDiagnosticInput(
		projectID,
		repositoryID,
		mergeSignal,
		fingerprint,
		errorCode,
		errorSummary,
		summary,
		config.ExpectedSignalKind,
		config.SignalKind,
		config.DefaultSummary,
	)
	if err != nil {
		return err
	}
	return s.recordRepositoryMergeSignalDiagnostic(ctx, signalInput, code, normalizedSummary)
}

func (s *Service) recordRepositoryMergeSignalDiagnostic(ctx context.Context, signalInput OnboardingSignalReconciliationInput, code string, summary string) error {
	repository, err := s.repository.GetRepository(ctx, signalInput.RepositoryID)
	if err != nil {
		return err
	}
	if repository.ProjectID != signalInput.ProjectID {
		return errs.ErrPreconditionFailed
	}
	if err := validateBootstrapRepository(repository); err != nil {
		return err
	}
	if signalInput.BaseBranch != strings.TrimSpace(repository.DefaultBranch) {
		return errs.ErrPreconditionFailed
	}
	providerSlug, err := repositoryProviderSlug(repository.Provider)
	if err != nil {
		return err
	}
	if err := validateBootstrapPolicyProviderTarget(providerSlug, repository, RepositoryBootstrapProviderTarget{
		ProviderSlug:         signalInput.ProviderSlug,
		RepositoryFullName:   signalInput.RepositoryFullName,
		ProviderRepositoryID: signalInput.ProviderRepositoryID,
	}); err != nil {
		return err
	}
	signal, err := s.onboardingSignalRecord(signalInput, enum.OnboardingSignalStatusNeedsReview, nil, nil)
	if err != nil {
		return err
	}
	signal.ErrorCode = code
	signal.ErrorSummary = summary
	completedAt := s.clock.Now()
	signal.CompletedAt = &completedAt
	_, err = s.repository.RecordOnboardingSignalReconciliation(ctx, signal)
	return err
}

func (s *Service) recordOnboardingSignalProcessing(ctx context.Context, input *OnboardingSignalReconciliationInput) error {
	if input == nil {
		return nil
	}
	signal, err := s.onboardingSignalRecord(*input, enum.OnboardingSignalStatusProcessing, nil, nil)
	if err != nil {
		return err
	}
	_, err = s.repository.RecordOnboardingSignalReconciliation(ctx, signal)
	return err
}

func (s *Service) recordOnboardingSignalImported(ctx context.Context, input *OnboardingSignalReconciliationInput, result BootstrapServicesPolicyImportResult) error {
	if input == nil {
		return nil
	}
	signal, err := s.onboardingSignalRecord(*input, enum.OnboardingSignalStatusImported, &result, nil)
	if err != nil {
		return err
	}
	_, err = s.repository.RecordOnboardingSignalReconciliation(ctx, signal)
	return err
}

func (s *Service) recordOnboardingSignalFailed(ctx context.Context, input *OnboardingSignalReconciliationInput, cause error) {
	if input == nil || cause == nil {
		return
	}
	signal, err := s.onboardingSignalRecord(*input, enum.OnboardingSignalStatusFailed, nil, cause)
	if err != nil {
		return
	}
	_, _ = s.repository.RecordOnboardingSignalReconciliation(ctx, signal)
}

func (s *Service) onboardingSignalRecord(
	input OnboardingSignalReconciliationInput,
	status enum.OnboardingSignalStatus,
	result *BootstrapServicesPolicyImportResult,
	cause error,
) (entity.OnboardingSignalReconciliation, error) {
	normalized, err := normalizeOnboardingSignalReconciliationInput(input)
	if err != nil {
		return entity.OnboardingSignalReconciliation{}, err
	}
	now := s.clock.Now()
	observedAt := now
	if normalized.ObservedAt != "" {
		parsed, err := parseRFC3339(normalized.ObservedAt)
		if err != nil {
			return entity.OnboardingSignalReconciliation{}, err
		}
		observedAt = parsed
	}
	signal := entity.OnboardingSignalReconciliation{
		Base: entity.Base{
			ID:        s.ids.New(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		ProjectID:            normalized.ProjectID,
		RepositoryID:         normalized.RepositoryID,
		SignalKind:           normalized.SignalKind,
		SignalKey:            normalized.SignalKey,
		SignalFingerprint:    normalized.SignalFingerprint,
		ProviderSlug:         normalized.ProviderSlug,
		RepositoryFullName:   normalized.RepositoryFullName,
		ProviderRepositoryID: normalized.ProviderRepositoryID,
		BaseBranch:           normalized.BaseBranch,
		SourceRef:            normalized.SourceRef,
		SourceCommitSHA:      normalized.SourceCommitSHA,
		ArtifactRef:          normalized.ArtifactRef,
		ArtifactDigest:       normalized.ArtifactDigest,
		ArtifactVersion:      normalized.ArtifactVersion,
		ContentHash:          normalized.ContentHash,
		Status:               status,
		Summary:              normalized.Summary,
		ObservedAt:           observedAt,
	}
	if result != nil {
		signal.ServicesPolicyID = &result.ServicesPolicy.ID
		signal.ServicesPolicyVersion = result.ServicesPolicy.PolicyVersion
		signal.Summary = firstNonEmpty(result.Summary, normalized.Summary)
		completedAt := now
		signal.CompletedAt = &completedAt
	}
	if cause != nil {
		code, summary := safeOnboardingSignalError(cause)
		signal.ErrorCode = code
		signal.ErrorSummary = summary
		completedAt := now
		signal.CompletedAt = &completedAt
	}
	return signal, nil
}

func normalizeRepositoryMergeSignalDiagnosticInput(
	projectID uuid.UUID,
	repositoryID uuid.UUID,
	signal BootstrapRepositoryMergeSignal,
	signalFingerprint string,
	errorCode string,
	errorSummary string,
	summaryText string,
	expectedSignalKind string,
	signalKind enum.OnboardingSignalKind,
	defaultSummary string,
) (OnboardingSignalReconciliationInput, string, string, error) {
	if projectID == uuid.Nil || repositoryID == uuid.Nil {
		return OnboardingSignalReconciliationInput{}, "", "", errs.ErrInvalidArgument
	}
	if err := validateOptionalSignalID(signal.SignalID); err != nil {
		return OnboardingSignalReconciliationInput{}, "", "", err
	}
	signalKey := strings.TrimSpace(signal.SignalKey)
	if signalKey == "" || strings.TrimSpace(signal.SignalKind) != expectedSignalKind {
		return OnboardingSignalReconciliationInput{}, "", "", errs.ErrInvalidArgument
	}
	baseBranch := normalizeBootstrapMergeBaseBranch(signal.BaseBranch)
	providerSourceRef := strings.TrimSpace(signal.SourceRef)
	mergeCommitSHA := strings.ToLower(strings.TrimSpace(signal.MergeCommitSHA))
	if baseBranch == "" || !validSafeProviderSourceRef(providerSourceRef) || !validGitCommitSHA(mergeCommitSHA) {
		return OnboardingSignalReconciliationInput{}, "", "", errs.ErrInvalidArgument
	}
	if strings.TrimSpace(signal.MergeObservedAt) != "" {
		if _, err := parseRFC3339(signal.MergeObservedAt); err != nil {
			return OnboardingSignalReconciliationInput{}, "", "", err
		}
	}
	if strings.TrimSpace(signal.MergedAt) != "" {
		if _, err := parseRFC3339(signal.MergedAt); err != nil {
			return OnboardingSignalReconciliationInput{}, "", "", err
		}
	}
	code, err := normalizeSafeOnboardingRef(errorCode, maxOnboardingErrorCodeLength, true)
	if err != nil {
		return OnboardingSignalReconciliationInput{}, "", "", err
	}
	summary := truncateSafeOnboardingSummary(firstNonEmpty(errorSummary, summaryText, defaultSummary))
	signalInput := OnboardingSignalReconciliationInput{
		ProjectID:            projectID,
		RepositoryID:         repositoryID,
		SignalKind:           signalKind,
		SignalKey:            signalKey,
		SignalFingerprint:    signalFingerprint,
		ProviderSlug:         signal.ProviderTarget.ProviderSlug,
		RepositoryFullName:   signal.ProviderTarget.RepositoryFullName,
		ProviderRepositoryID: signal.ProviderTarget.ProviderRepositoryID,
		BaseBranch:           baseBranch,
		SourceRef:            "refs/heads/" + baseBranch,
		SourceCommitSHA:      mergeCommitSHA,
		Summary:              truncateSafeOnboardingSummary(firstNonEmpty(summaryText, summary)),
		ObservedAt:           firstNonEmpty(strings.TrimSpace(signal.MergeObservedAt), strings.TrimSpace(signal.MergedAt)),
	}
	normalized, err := normalizeOnboardingSignalReconciliationInput(signalInput)
	if err != nil {
		return OnboardingSignalReconciliationInput{}, "", "", err
	}
	return normalized, code, summary, nil
}

func normalizeOnboardingSignalReconciliationInput(input OnboardingSignalReconciliationInput) (OnboardingSignalReconciliationInput, error) {
	projectID := input.ProjectID
	repositoryID := input.RepositoryID
	signalKey, err := normalizeSafeOnboardingRef(input.SignalKey, maxOnboardingSignalKeyLength, true)
	if err != nil {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	signalFingerprint := strings.ToLower(strings.TrimSpace(input.SignalFingerprint))
	if !validSHA256ContentHash(signalFingerprint) {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	providerSlug, err := normalizeSafeOnboardingRef(input.ProviderSlug, maxOnboardingProviderSlugLength, true)
	if err != nil {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	repositoryFullName, err := normalizeOnboardingRepositoryFullName(input.RepositoryFullName)
	if err != nil {
		return OnboardingSignalReconciliationInput{}, err
	}
	providerRepositoryID, err := normalizeSafeOnboardingRef(input.ProviderRepositoryID, maxOnboardingProviderRefLength, false)
	if err != nil {
		return OnboardingSignalReconciliationInput{}, err
	}
	baseBranch, err := normalizeSafeOnboardingBranch(input.BaseBranch)
	if err != nil {
		return OnboardingSignalReconciliationInput{}, err
	}
	sourceRef, err := normalizeSafeOnboardingRef(input.SourceRef, maxOnboardingProviderRefLength, false)
	if err != nil {
		return OnboardingSignalReconciliationInput{}, err
	}
	sourceCommitSHA := strings.ToLower(strings.TrimSpace(input.SourceCommitSHA))
	if sourceCommitSHA != "" && !validGitCommitSHA(sourceCommitSHA) {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	artifactRef, err := normalizeSafeOnboardingRef(input.ArtifactRef, maxOnboardingArtifactRefLength, false)
	if err != nil {
		return OnboardingSignalReconciliationInput{}, err
	}
	artifactDigest := strings.ToLower(strings.TrimSpace(input.ArtifactDigest))
	if artifactDigest != "" && !validSHA256ContentHash(artifactDigest) {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	artifactVersion, err := normalizeSafeOnboardingRef(input.ArtifactVersion, maxOnboardingArtifactVersionLength, false)
	if err != nil {
		return OnboardingSignalReconciliationInput{}, err
	}
	contentHash := strings.ToLower(strings.TrimSpace(input.ContentHash))
	if contentHash != "" && !validSHA256ContentHash(contentHash) {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	if projectID == uuid.Nil || repositoryID == uuid.Nil {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	if input.SignalKind != enum.OnboardingSignalKindBootstrapMerge &&
		input.SignalKind != enum.OnboardingSignalKindAdoptionScan &&
		input.SignalKind != enum.OnboardingSignalKindAdoptionMerge {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	return OnboardingSignalReconciliationInput{
		ProjectID:            projectID,
		RepositoryID:         repositoryID,
		SignalKind:           input.SignalKind,
		SignalKey:            signalKey,
		SignalFingerprint:    signalFingerprint,
		ProviderSlug:         providerSlug,
		RepositoryFullName:   repositoryFullName,
		ProviderRepositoryID: providerRepositoryID,
		BaseBranch:           baseBranch,
		SourceRef:            sourceRef,
		SourceCommitSHA:      sourceCommitSHA,
		ArtifactRef:          artifactRef,
		ArtifactDigest:       artifactDigest,
		ArtifactVersion:      artifactVersion,
		ContentHash:          contentHash,
		Summary:              truncateSafeOnboardingSummary(input.Summary),
		ObservedAt:           strings.TrimSpace(input.ObservedAt),
	}, nil
}

func normalizeSafeOnboardingBranch(text string) (string, error) {
	branch, err := normalizeSafeOnboardingRef(text, maxOnboardingProviderRefLength, false)
	if err != nil || branch == "" {
		return branch, err
	}
	if !validBootstrapBranchName(branch) {
		return "", errs.ErrInvalidArgument
	}
	return branch, nil
}

func normalizeOnboardingRepositoryFullName(text string) (string, error) {
	fullName, err := normalizeSafeOnboardingRef(text, maxOnboardingRepositoryFullNameLength, true)
	if err != nil {
		return "", err
	}
	owner, name, ok := strings.Cut(fullName, "/")
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	lastSlash := strings.LastIndex(fullName, "/")
	if lastSlash > 0 && lastSlash < len(fullName)-1 {
		owner = fullName[:lastSlash]
		name = fullName[lastSlash+1:]
	}
	if !validProviderOwnerRef(owner) || !validProviderRepositoryName(name) {
		return "", errs.ErrInvalidArgument
	}
	return fullName, nil
}

func normalizeSafeOnboardingRef(text string, limit int, required bool) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		if required {
			return "", errs.ErrInvalidArgument
		}
		return "", nil
	}
	if len(trimmed) > limit || !validSafeOnboardingRef(trimmed) {
		return "", errs.ErrInvalidArgument
	}
	return trimmed, nil
}

func validSafeOnboardingRef(text string) bool {
	for _, char := range text {
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '-', '_', '.', '/', ':', '@', '+', '=', ',', '#', '%', '?', '&':
			continue
		default:
			return false
		}
	}
	return true
}

func safeOnboardingSignalError(cause error) (string, string) {
	code := "internal"
	switch {
	case errors.Is(cause, errs.ErrInvalidArgument):
		code = "invalid_argument"
	case errors.Is(cause, errs.ErrForbidden):
		code = "permission_denied"
	case errors.Is(cause, errs.ErrNotFound):
		code = "not_found"
	case errors.Is(cause, errs.ErrAlreadyExists):
		code = "already_exists"
	case errors.Is(cause, errs.ErrPreconditionFailed):
		code = "failed_precondition"
	case errors.Is(cause, errs.ErrConflict):
		code = "conflict"
	case errors.Is(cause, errs.ErrDependencyUnavailable):
		code = "unavailable"
	}
	return code, truncateSafeOnboardingSummary("onboarding signal reconciliation failed: " + code)
}

func truncateSafeOnboardingSummary(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= safeOnboardingErrorSummaryLimit {
		return trimmed
	}
	return trimmed[:safeOnboardingErrorSummaryLimit]
}
