package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
)

// ImportServicesPolicy stores a checked services.yaml projection.
func (s *Service) ImportServicesPolicy(ctx context.Context, input ImportServicesPolicyInput) (entity.ServicesPolicy, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.ServicesPolicy{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionPolicyImport, projectScopedResource(projectAggregateServicesPolicy, input.ProjectID)); err != nil {
		return entity.ServicesPolicy{}, err
	}
	if result, ok, err := s.findCommandResult(ctx, input.Meta, projectOperationImportServicesPolicy, projectAggregateServicesPolicy); ok || err != nil {
		if err != nil {
			return entity.ServicesPolicy{}, err
		}
		return s.repository.GetServicesPolicy(ctx, input.ProjectID, &result.AggregateID)
	}
	now := s.clock.Now()
	validationStatus := defaultValidationStatus(input.ValidationStatus)
	projection, err := buildServicesPolicyProjection(input, validationStatus)
	if err != nil {
		return entity.ServicesPolicy{}, err
	}
	policy := entity.ServicesPolicy{
		Base:               newBase(s.ids.New(), now),
		ProjectID:          input.ProjectID,
		SourceRepositoryID: input.SourceRepositoryID,
		SourcePath:         strings.TrimSpace(input.SourcePath),
		SourceRef:          strings.TrimSpace(input.SourceRef),
		SourceCommitSHA:    strings.TrimSpace(input.SourceCommitSHA),
		SourceBlobSHA:      strings.TrimSpace(input.SourceBlobSHA),
		ContentHash:        strings.TrimSpace(input.ContentHash),
		ValidatedPayload:   projection.payload,
		ValidationStatus:   validationStatus,
		ProjectionStatus:   projectionStatusForValidation(validationStatus),
		ImportedAt:         now,
	}
	if policy.SourcePath == "" || policy.SourceCommitSHA == "" || policy.ContentHash == "" {
		return entity.ServicesPolicy{}, errs.ErrInvalidArgument
	}
	descriptors := s.prepareServiceDescriptors(policy, projection.descriptors, now)
	result, err := commandResult(input.Meta, projectOperationImportServicesPolicy, projectAggregateServicesPolicy, policy.ID, now)
	if err != nil {
		return entity.ServicesPolicy{}, err
	}
	imported, err := s.repository.ImportServicesPolicy(ctx, policy, descriptors, *result, s.servicesPolicyEvent)
	if err != nil {
		return entity.ServicesPolicy{}, err
	}
	return imported, nil
}

// GetServicesPolicy returns active or concrete checked services policy.
func (s *Service) GetServicesPolicy(ctx context.Context, input GetServicesPolicyInput) (entity.ServicesPolicy, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.ServicesPolicy{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, projectActionPolicyRead, projectScopedResource(projectAggregateServicesPolicy, input.ProjectID)); err != nil {
		return entity.ServicesPolicy{}, err
	}
	return s.repository.GetServicesPolicy(ctx, input.ProjectID, input.ServicesPolicyID)
}

// ListServiceDescriptors returns typed services from checked policy.
func (s *Service) ListServiceDescriptors(ctx context.Context, input ListServiceDescriptorsInput) (ListServiceDescriptorsResult, error) {
	if err := s.authorizeProjectQuery(ctx, input.ProjectID, input.Meta, projectActionPolicyRead, projectAggregateServicesPolicy); err != nil {
		return ListServiceDescriptorsResult{}, err
	}
	descriptors, page, err := s.repository.ListServiceDescriptors(ctx, query.ServiceDescriptorFilter{
		ProjectID:    input.ProjectID,
		RepositoryID: input.RepositoryID,
		ServiceKeys:  input.ServiceKeys,
		Statuses:     input.Statuses,
		Page:         input.Page,
	})
	if err != nil {
		return ListServiceDescriptorsResult{}, err
	}
	return ListServiceDescriptorsResult{ServiceDescriptors: descriptors, Page: page}, nil
}

// CreatePolicyEditProposal stores a request to change services.yaml through provider PR.
func (s *Service) CreatePolicyEditProposal(ctx context.Context, input CreatePolicyEditProposalInput) (entity.PolicyEditProposal, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.PolicyEditProposal{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionPolicyPropose, projectScopedResource(projectAggregateServicesPolicy, input.ProjectID)); err != nil {
		return entity.PolicyEditProposal{}, err
	}
	if proposal, ok, err := findScopedCommandReplay(s, ctx, input.Meta, projectOperationPolicyEditProposal, projectAggregatePolicyEditProposal, input.ProjectID, s.repository.GetPolicyEditProposal, policyEditProposalProjectID); ok || err != nil {
		return proposal, err
	}
	now := s.clock.Now()
	proposal := entity.PolicyEditProposal{
		ID:               s.ids.New(),
		ProjectID:        input.ProjectID,
		RepositoryID:     input.RepositoryID,
		SourcePath:       strings.TrimSpace(input.SourcePath),
		RequestedChanges: input.RequestedChanges,
		Status:           projectProposalStatusPending,
		CreatedAt:        now,
	}
	if proposal.RepositoryID == uuid.Nil || proposal.SourcePath == "" {
		return entity.PolicyEditProposal{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(input.Meta, projectOperationPolicyEditProposal, projectAggregatePolicyEditProposal, proposal.ID, now)
	if err != nil {
		return entity.PolicyEditProposal{}, err
	}
	if err := s.repository.CreatePolicyEditProposal(ctx, proposal, *result); err != nil {
		return entity.PolicyEditProposal{}, err
	}
	return proposal, nil
}

// CreatePolicyOverride creates a time-bound operator override.
func (s *Service) CreatePolicyOverride(ctx context.Context, input CreatePolicyOverrideInput) (entity.PolicyOverride, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.PolicyOverride{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionPolicyOverride, projectScopedResource(projectAggregatePolicyOverride, input.ProjectID)); err != nil {
		return entity.PolicyOverride{}, err
	}
	if override, ok, err := findScopedCommandReplay(s, ctx, input.Meta, projectOperationPolicyOverride, projectAggregatePolicyOverride, input.ProjectID, s.repository.GetPolicyOverride, policyOverrideProjectID); ok || err != nil {
		return override, err
	}
	expiresAt, err := parseRFC3339(input.ExpiresAt)
	if err != nil {
		return entity.PolicyOverride{}, err
	}
	now := s.clock.Now()
	override := entity.PolicyOverride{
		Base:              newBase(s.ids.New(), now),
		ProjectID:         input.ProjectID,
		TargetType:        input.TargetType,
		TargetID:          input.TargetID,
		Payload:           input.Payload,
		Reason:            strings.TrimSpace(input.Meta.Reason),
		Status:            enum.PolicyOverrideStatusActive,
		ExpiresAt:         expiresAt,
		CreatedByActorRef: actorRef(input.Meta.Actor),
	}
	if override.TargetType == "" || len(override.Payload) == 0 || !json.Valid(override.Payload) || override.Reason == "" || !override.ExpiresAt.After(now) {
		return entity.PolicyOverride{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(input.Meta, projectOperationPolicyOverride, projectAggregatePolicyOverride, override.ID, now)
	if err != nil {
		return entity.PolicyOverride{}, err
	}
	event, err := s.policyOverrideEvent(projectEventPolicyOverrideCreated, override)
	if err != nil {
		return entity.PolicyOverride{}, err
	}
	if err := s.repository.CreatePolicyOverride(ctx, override, event, *result); err != nil {
		return entity.PolicyOverride{}, err
	}
	return override, nil
}

// CancelPolicyOverride cancels an active operator override before expiration.
func (s *Service) CancelPolicyOverride(ctx context.Context, input CancelPolicyOverrideInput) (entity.PolicyOverride, error) {
	if input.PolicyOverrideID == uuid.Nil {
		return entity.PolicyOverride{}, errs.ErrInvalidArgument
	}
	current, err := s.repository.GetPolicyOverride(ctx, input.PolicyOverrideID)
	if err != nil {
		return entity.PolicyOverride{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionPolicyOverrideCancel, projectScopedResource(projectAggregatePolicyOverride, current.ProjectID)); err != nil {
		return entity.PolicyOverride{}, err
	}
	if replay, ok, err := findScopedCommandReplay(s, ctx, input.Meta, projectOperationCancelPolicyOverride, projectAggregatePolicyOverride, current.ProjectID, s.repository.GetPolicyOverride, policyOverrideProjectID); ok || err != nil {
		return replay, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.PolicyOverride{}, err
	}
	if current.Status != enum.PolicyOverrideStatusActive {
		return entity.PolicyOverride{}, errs.ErrPreconditionFailed
	}
	now := s.clock.Now()
	cancelled := current
	cancelled.Base = updatedBase(current.Base, now)
	cancelled.Status = enum.PolicyOverrideStatusCancelled
	result, err := commandResult(input.Meta, projectOperationCancelPolicyOverride, projectAggregatePolicyOverride, cancelled.ID, now)
	if err != nil {
		return entity.PolicyOverride{}, err
	}
	event, err := s.policyOverrideEvent(projectEventPolicyOverrideCancelled, cancelled)
	if err != nil {
		return entity.PolicyOverride{}, err
	}
	if err := s.repository.CancelPolicyOverride(ctx, cancelled, previousVersion, event, result); err != nil {
		return entity.PolicyOverride{}, err
	}
	return cancelled, nil
}

// ListPolicyOverrides returns operator overrides matching filter.
func (s *Service) ListPolicyOverrides(ctx context.Context, input ListPolicyOverridesInput) (ListPolicyOverridesResult, error) {
	if err := s.authorizeProjectQuery(ctx, input.ProjectID, input.Meta, projectActionPolicyOverrideRead, projectAggregatePolicyOverride); err != nil {
		return ListPolicyOverridesResult{}, err
	}
	activeAt := s.clock.Now()
	overrides, page, err := s.repository.ListPolicyOverrides(ctx, query.PolicyOverrideFilter{
		ProjectID:   input.ProjectID,
		TargetTypes: input.TargetTypes,
		TargetID:    input.TargetID,
		Statuses:    input.Statuses,
		ActiveOnly:  input.ActiveOnly,
		ActiveAt:    &activeAt,
		Page:        input.Page,
	})
	if err != nil {
		return ListPolicyOverridesResult{}, err
	}
	return ListPolicyOverridesResult{PolicyOverrides: overrides, Page: page}, nil
}

func (s *Service) prepareServiceDescriptors(policy entity.ServicesPolicy, descriptors []entity.ServiceDescriptor, now time.Time) []entity.ServiceDescriptor {
	result := make([]entity.ServiceDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		descriptor.Base = newBase(s.ids.New(), now)
		descriptor.ProjectID = policy.ProjectID
		descriptor.ServicesPolicyID = policy.ID
		if descriptor.Status == "" {
			descriptor.Status = enum.ServiceStatusActive
		}
		result = append(result, descriptor)
	}
	return result
}

func (s *Service) servicesPolicyEvent(policy entity.ServicesPolicy) (entity.OutboxEvent, error) {
	options := []projectEventPayloadOption{
		payloadProjectID(policy.ProjectID),
		payloadField(projectPayloadPolicyID, policy.ID.String()),
		payloadPolicyVersion(policy.PolicyVersion),
		payloadField(projectPayloadSourceCommit, policy.SourceCommitSHA),
		payloadField(projectPayloadContentHash, policy.ContentHash),
	}
	if policy.SourceBlobSHA != "" {
		options = append(options, payloadField(projectPayloadSourceBlob, policy.SourceBlobSHA))
	}
	return s.aggregateEvent(projectEventServicesPolicyImported, projectAggregateServicesPolicy, policy.ID, policy.ImportedAt, options...)
}

func (s *Service) policyOverrideEvent(eventType string, override entity.PolicyOverride) (entity.OutboxEvent, error) {
	return s.aggregateEvent(
		eventType,
		projectAggregatePolicyOverride,
		override.ID,
		override.UpdatedAt,
		payloadProjectID(override.ProjectID),
		payloadField(projectPayloadOverrideID, override.ID.String()),
		payloadField(projectPayloadTargetType, string(override.TargetType)),
		payloadField(projectPayloadExpiresAt, override.ExpiresAt.Format(time.RFC3339Nano)),
	)
}
