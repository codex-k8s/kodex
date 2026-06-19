package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

// GetProjectOnboardingStatus returns safe readiness for manual bootstrap/adoption.
func (s *Service) GetProjectOnboardingStatus(ctx context.Context, input GetProjectOnboardingStatusInput) (ProjectOnboardingStatusResult, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return onboardingStatus(enum.ProjectOnboardingStatusInvalidInput, "invalid_project_id"), nil
	}
	if err := s.authorizeQuery(ctx, input.Meta, projectActionPolicyRead, projectScopedResource(projectAggregateServicesPolicy, input.ProjectID)); err != nil {
		return ProjectOnboardingStatusResult{}, err
	}

	project, err := s.repository.GetProject(ctx, input.ProjectID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return onboardingStatus(enum.ProjectOnboardingStatusProjectNotFound, "project_not_found"), nil
		}
		return ProjectOnboardingStatusResult{}, err
	}
	result := onboardingStatus(enum.ProjectOnboardingStatusReady, "")
	result.Project = &project
	if project.Status != enum.ProjectStatusActive {
		result.Status = enum.ProjectOnboardingStatusProjectNotActive
		result.SafeReason = "project_not_active"
		result.Summary = onboardingSummary(result)
		return result, nil
	}

	repository, status, reason, err := s.onboardingRepositoryStatus(ctx, input)
	if err != nil {
		return ProjectOnboardingStatusResult{}, err
	}
	if repository != nil {
		result.Repository = repository
	}
	if status != enum.ProjectOnboardingStatusReady {
		result.Status = status
		result.SafeReason = reason
		result.Summary = onboardingSummary(result)
		return result, nil
	}

	policy, status, reason, err := s.onboardingServicesPolicyStatus(ctx, input)
	if err != nil {
		return ProjectOnboardingStatusResult{}, err
	}
	if policy != nil {
		result.ServicesPolicy = policy
	}
	if status != enum.ProjectOnboardingStatusReady {
		result.Status = status
		result.SafeReason = reason
		result.Summary = onboardingSummary(result)
		return result, nil
	}

	descriptors, status, reason, err := s.onboardingServiceDescriptorsStatus(ctx, input)
	if err != nil {
		return ProjectOnboardingStatusResult{}, err
	}
	result.ServiceDescriptors = descriptors
	if status != enum.ProjectOnboardingStatusReady {
		result.Status = status
		result.SafeReason = reason
	}
	result.Summary = onboardingSummary(result)
	return result, nil
}

func (s *Service) onboardingRepositoryStatus(ctx context.Context, input GetProjectOnboardingStatusInput) (*entity.RepositoryBinding, enum.ProjectOnboardingStatus, string, error) {
	if input.RepositoryID == nil {
		return nil, enum.ProjectOnboardingStatusReady, "", nil
	}
	repository, err := s.repository.GetRepository(ctx, *input.RepositoryID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return nil, enum.ProjectOnboardingStatusRepositoryBindingNotFound, "repository_binding_not_found", nil
		}
		return nil, "", "", err
	}
	if repository.ProjectID != input.ProjectID {
		return &repository, enum.ProjectOnboardingStatusRepositoryBindingConflict, "repository_binding_project_mismatch", nil
	}
	if repository.Status != enum.RepositoryStatusActive {
		return &repository, enum.ProjectOnboardingStatusRepositoryBindingNotActive, "repository_binding_not_active", nil
	}
	return &repository, enum.ProjectOnboardingStatusReady, "", nil
}

func (s *Service) onboardingServicesPolicyStatus(ctx context.Context, input GetProjectOnboardingStatusInput) (*entity.ServicesPolicy, enum.ProjectOnboardingStatus, string, error) {
	policy, err := s.repository.GetServicesPolicy(ctx, input.ProjectID, input.ExpectedServicesPolicyID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return nil, enum.ProjectOnboardingStatusServicesPolicyNotFound, "services_policy_not_found", nil
		}
		return nil, "", "", err
	}
	if policy.ValidationStatus == enum.ServicesPolicyValidationStale {
		return &policy, enum.ProjectOnboardingStatusServicesPolicyStale, "services_policy_validation_stale", nil
	}
	if !onboardingPolicyReady(policy) {
		return &policy, enum.ProjectOnboardingStatusServicesPolicyNotReady, "services_policy_not_ready", nil
	}
	if reason := onboardingPolicyStaleReason(policy, input); reason != "" {
		return &policy, enum.ProjectOnboardingStatusServicesPolicyStale, reason, nil
	}
	return &policy, enum.ProjectOnboardingStatusReady, "", nil
}

func (s *Service) onboardingServiceDescriptorsStatus(ctx context.Context, input GetProjectOnboardingStatusInput) ([]entity.ServiceDescriptor, enum.ProjectOnboardingStatus, string, error) {
	serviceKeys := normalizeServiceKeys(input.ServiceKeys)
	page := value.PageRequest{}
	if len(serviceKeys) > 0 {
		page.PageSize = int32(len(serviceKeys))
	}
	descriptors, _, err := s.repository.ListServiceDescriptors(ctx, query.ServiceDescriptorFilter{
		ProjectID:    input.ProjectID,
		RepositoryID: input.RepositoryID,
		ServiceKeys:  serviceKeys,
		Statuses:     []enum.ServiceStatus{enum.ServiceStatusActive},
		Page:         page,
	})
	if err != nil {
		return nil, "", "", err
	}
	if len(descriptors) == 0 {
		return descriptors, enum.ProjectOnboardingStatusServiceDescriptorsNotFound, "service_descriptors_not_found", nil
	}
	if missing := missingServiceDescriptorKey(serviceKeys, descriptors); missing != "" {
		return descriptors, enum.ProjectOnboardingStatusServiceDescriptorsNotFound, "service_descriptor_not_found:" + missing, nil
	}
	return descriptors, enum.ProjectOnboardingStatusReady, "", nil
}

func onboardingPolicyReady(policy entity.ServicesPolicy) bool {
	if policy.ValidationStatus != enum.ServicesPolicyValidationValid {
		return false
	}
	return policy.ProjectionStatus == enum.ServicesPolicyProjectionSynced || policy.ProjectionStatus == enum.ServicesPolicyProjectionOverridden
}

func onboardingPolicyStaleReason(policy entity.ServicesPolicy, input GetProjectOnboardingStatusInput) string {
	if input.RepositoryID != nil && policy.SourceRepositoryID != nil && *policy.SourceRepositoryID != *input.RepositoryID {
		return "services_policy_repository_mismatch"
	}
	if expected := strings.TrimSpace(input.ExpectedSourceRef); expected != "" && strings.TrimSpace(policy.SourceRef) != expected {
		return "services_policy_source_ref_mismatch"
	}
	if expected := strings.TrimSpace(input.ExpectedSourceCommitSHA); expected != "" && strings.TrimSpace(policy.SourceCommitSHA) != expected {
		return "services_policy_source_commit_mismatch"
	}
	if expected := strings.TrimSpace(input.ExpectedContentHash); expected != "" && strings.TrimSpace(policy.ContentHash) != expected {
		return "services_policy_content_hash_mismatch"
	}
	if input.ExpectedServicesPolicyVersion != nil && policy.PolicyVersion != *input.ExpectedServicesPolicyVersion {
		return "services_policy_version_mismatch"
	}
	return ""
}

func missingServiceDescriptorKey(keys []string, descriptors []entity.ServiceDescriptor) string {
	if len(keys) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		seen[descriptor.ServiceKey] = struct{}{}
	}
	for _, key := range keys {
		if _, ok := seen[key]; !ok {
			return key
		}
	}
	return ""
}

func onboardingStatus(status enum.ProjectOnboardingStatus, reason string) ProjectOnboardingStatusResult {
	result := ProjectOnboardingStatusResult{Status: status, SafeReason: reason}
	result.Summary = onboardingSummary(result)
	return result
}

func onboardingSummary(result ProjectOnboardingStatusResult) string {
	status := string(result.Status)
	if result.SafeReason != "" {
		return fmt.Sprintf("project onboarding %s: %s", status, result.SafeReason)
	}
	if result.ServicesPolicy != nil {
		return fmt.Sprintf("project onboarding ready policy_version=%d descriptors=%d", result.ServicesPolicy.PolicyVersion, len(result.ServiceDescriptors))
	}
	return "project onboarding " + status
}
