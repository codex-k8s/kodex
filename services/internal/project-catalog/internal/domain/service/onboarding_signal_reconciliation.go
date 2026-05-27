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

func normalizeOnboardingSignalReconciliationInput(input OnboardingSignalReconciliationInput) (OnboardingSignalReconciliationInput, error) {
	projectID := input.ProjectID
	repositoryID := input.RepositoryID
	if strings.TrimSpace(input.SignalKey) == "" ||
		strings.TrimSpace(input.SignalFingerprint) == "" ||
		strings.TrimSpace(input.ProviderSlug) == "" ||
		strings.TrimSpace(input.RepositoryFullName) == "" {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	if projectID == uuid.Nil || repositoryID == uuid.Nil {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	if input.SignalKind != enum.OnboardingSignalKindBootstrapMerge && input.SignalKind != enum.OnboardingSignalKindAdoptionScan {
		return OnboardingSignalReconciliationInput{}, errs.ErrInvalidArgument
	}
	return OnboardingSignalReconciliationInput{
		ProjectID:            projectID,
		RepositoryID:         repositoryID,
		SignalKind:           input.SignalKind,
		SignalKey:            strings.TrimSpace(input.SignalKey),
		SignalFingerprint:    strings.TrimSpace(input.SignalFingerprint),
		ProviderSlug:         strings.TrimSpace(input.ProviderSlug),
		RepositoryFullName:   strings.TrimSpace(input.RepositoryFullName),
		ProviderRepositoryID: strings.TrimSpace(input.ProviderRepositoryID),
		BaseBranch:           strings.TrimSpace(input.BaseBranch),
		SourceRef:            strings.TrimSpace(input.SourceRef),
		SourceCommitSHA:      strings.ToLower(strings.TrimSpace(input.SourceCommitSHA)),
		ArtifactRef:          strings.TrimSpace(input.ArtifactRef),
		ArtifactDigest:       strings.TrimSpace(input.ArtifactDigest),
		ArtifactVersion:      strings.TrimSpace(input.ArtifactVersion),
		ContentHash:          strings.TrimSpace(input.ContentHash),
		Summary:              truncateSafeOnboardingSummary(input.Summary),
		ObservedAt:           strings.TrimSpace(input.ObservedAt),
	}, nil
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
	return code, truncateSafeOnboardingSummary("bootstrap merge reconciliation failed: " + code)
}

func truncateSafeOnboardingSummary(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= safeOnboardingErrorSummaryLimit {
		return trimmed
	}
	return trimmed[:safeOnboardingErrorSummaryLimit]
}
