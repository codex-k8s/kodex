package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

// PutBranchRules creates or updates branch rules.
func (s *Service) PutBranchRules(ctx context.Context, input PutBranchRulesInput) (entity.BranchRules, error) {
	id, previous, err := putIdentity(input.BranchRulesID, input.Meta, s.ids.New())
	if err != nil {
		return entity.BranchRules{}, err
	}
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.BranchRules{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionBranchRulesUpdate, projectScopedResource(projectAggregateBranchRules, input.ProjectID)); err != nil {
		return entity.BranchRules{}, err
	}
	if replay, ok, err := findScopedCommandReplay(s, ctx, input.Meta, projectOperationPutBranchRules, projectAggregateBranchRules, input.ProjectID, s.repository.GetBranchRules, branchRulesProjectID); ok || err != nil {
		if err != nil {
			return entity.BranchRules{}, err
		}
		return replay, nil
	}
	now := s.clock.Now()
	rules := entity.BranchRules{
		Base:           newBase(id, now),
		ProjectID:      input.ProjectID,
		RepositoryID:   input.RepositoryID,
		Pattern:        strings.TrimSpace(input.Pattern),
		RequiredChecks: trimStrings(input.RequiredChecks),
		MergePolicy:    defaultMergePolicy(input.MergePolicy),
		Status:         defaultBranchRulesStatus(input.Status),
	}
	if previous != nil {
		rules.Version = *previous + 1
	}
	if rules.Pattern == "" {
		return entity.BranchRules{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(input.Meta, projectOperationPutBranchRules, projectAggregateBranchRules, rules.ID, now)
	if err != nil {
		return entity.BranchRules{}, err
	}
	event, err := s.branchRulesEvent(branchRulesEventType(previous == nil, rules.Status), rules)
	if err != nil {
		return entity.BranchRules{}, err
	}
	if err := s.repository.PutBranchRules(ctx, rules, previous, event, result); err != nil {
		return entity.BranchRules{}, err
	}
	return rules, nil
}

// GetBranchRules returns branch rules by id.
func (s *Service) GetBranchRules(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.BranchRules, error) {
	return readProjectScopedAggregate(s, ctx, id, meta, projectActionBranchRulesRead, projectAggregateBranchRules, s.repository.GetBranchRules, branchRulesProjectID)
}

// ListBranchRules returns branch rules matching filter.
func (s *Service) ListBranchRules(ctx context.Context, input ListBranchRulesInput) (ListBranchRulesResult, error) {
	return listProjectScoped(s, ctx, input.ProjectID, input.Meta, projectActionBranchRulesRead, projectAggregateBranchRules,
		func(ctx context.Context) ([]entity.BranchRules, value.PageResult, error) {
			return s.repository.ListBranchRules(ctx, query.BranchRulesFilter{
				ProjectID:    input.ProjectID,
				RepositoryID: input.RepositoryID,
				Statuses:     input.Statuses,
				Page:         input.Page,
			})
		},
		func(rules []entity.BranchRules, page value.PageResult) ListBranchRulesResult {
			return ListBranchRulesResult{BranchRules: rules, Page: page}
		},
	)
}

// PutReleasePolicy creates or updates a release policy.
func (s *Service) PutReleasePolicy(ctx context.Context, input PutReleasePolicyInput) (entity.ReleasePolicy, error) {
	id, previous, err := putIdentity(input.ReleasePolicyID, input.Meta, s.ids.New())
	if err != nil {
		return entity.ReleasePolicy{}, err
	}
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.ReleasePolicy{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionReleasePolicyUpdate, projectScopedResource(projectAggregateReleasePolicy, input.ProjectID)); err != nil {
		return entity.ReleasePolicy{}, err
	}
	if replay, ok, err := findScopedCommandReplay(s, ctx, input.Meta, projectOperationPutReleasePolicy, projectAggregateReleasePolicy, input.ProjectID, s.repository.GetReleasePolicy, releasePolicyProjectID); ok || err != nil {
		if err != nil {
			return entity.ReleasePolicy{}, err
		}
		return replay, nil
	}
	now := s.clock.Now()
	policy := entity.ReleasePolicy{
		Base:            newBase(id, now),
		ProjectID:       input.ProjectID,
		Name:            strings.TrimSpace(input.Name),
		BranchPattern:   strings.TrimSpace(input.BranchPattern),
		RolloutStrategy: defaultRolloutStrategy(input.RolloutStrategy),
		RollbackPolicy:  defaultRollbackPolicy(input.RollbackPolicy),
		RiskProfileRef:  strings.TrimSpace(input.RiskProfileRef),
		Status:          defaultReleasePolicyStatus(input.Status),
	}
	if previous != nil {
		policy.Version = *previous + 1
	}
	if policy.Name == "" || policy.BranchPattern == "" {
		return entity.ReleasePolicy{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(input.Meta, projectOperationPutReleasePolicy, projectAggregateReleasePolicy, policy.ID, now)
	if err != nil {
		return entity.ReleasePolicy{}, err
	}
	event, err := s.releasePolicyEvent(releasePolicyEventType(previous == nil, policy.Status), policy)
	if err != nil {
		return entity.ReleasePolicy{}, err
	}
	if err := s.repository.PutReleasePolicy(ctx, policy, previous, event, result); err != nil {
		return entity.ReleasePolicy{}, err
	}
	return policy, nil
}

// GetReleasePolicy returns release policy by id.
func (s *Service) GetReleasePolicy(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.ReleasePolicy, error) {
	return readProjectScopedAggregate(s, ctx, id, meta, projectActionReleasePolicyRead, projectAggregateReleasePolicy, s.repository.GetReleasePolicy, releasePolicyProjectID)
}

// ListReleasePolicies returns release policies matching filter.
func (s *Service) ListReleasePolicies(ctx context.Context, input ListReleasePoliciesInput) (ListReleasePoliciesResult, error) {
	return listProjectScoped(s, ctx, input.ProjectID, input.Meta, projectActionReleasePolicyRead, projectAggregateReleasePolicy,
		func(ctx context.Context) ([]entity.ReleasePolicy, value.PageResult, error) {
			return s.repository.ListReleasePolicies(ctx, query.ReleasePolicyFilter{
				ProjectID: input.ProjectID,
				Statuses:  input.Statuses,
				Page:      input.Page,
			})
		},
		func(policies []entity.ReleasePolicy, page value.PageResult) ListReleasePoliciesResult {
			return ListReleasePoliciesResult{ReleasePolicies: policies, Page: page}
		},
	)
}

// PutReleaseLine creates or updates a release line.
func (s *Service) PutReleaseLine(ctx context.Context, input PutReleaseLineInput) (entity.ReleaseLine, error) {
	id, previous, err := putIdentity(input.ReleaseLineID, input.Meta, s.ids.New())
	if err != nil {
		return entity.ReleaseLine{}, err
	}
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.ReleaseLine{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionReleaseLineUpdate, projectScopedResource(projectAggregateReleaseLine, input.ProjectID)); err != nil {
		return entity.ReleaseLine{}, err
	}
	if replay, ok, err := findScopedCommandReplay(s, ctx, input.Meta, projectOperationPutReleaseLine, projectAggregateReleaseLine, input.ProjectID, s.repository.GetReleaseLine, releaseLineProjectID); ok || err != nil {
		if err != nil {
			return entity.ReleaseLine{}, err
		}
		return replay, nil
	}
	now := s.clock.Now()
	line := entity.ReleaseLine{
		Base:            newBase(id, now),
		ProjectID:       input.ProjectID,
		ReleasePolicyID: input.ReleasePolicyID,
		Name:            strings.TrimSpace(input.Name),
		BranchPattern:   strings.TrimSpace(input.BranchPattern),
		Status:          defaultReleasePolicyStatus(input.Status),
	}
	if previous != nil {
		line.Version = *previous + 1
	}
	if line.ReleasePolicyID == uuid.Nil || line.Name == "" || line.BranchPattern == "" {
		return entity.ReleaseLine{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(input.Meta, projectOperationPutReleaseLine, projectAggregateReleaseLine, line.ID, now)
	if err != nil {
		return entity.ReleaseLine{}, err
	}
	event, err := s.releaseLineEvent(releaseLineEventType(previous == nil, line.Status), line)
	if err != nil {
		return entity.ReleaseLine{}, err
	}
	if err := s.repository.PutReleaseLine(ctx, line, previous, event, result); err != nil {
		return entity.ReleaseLine{}, err
	}
	return line, nil
}

// GetReleaseLine returns release line by id.
func (s *Service) GetReleaseLine(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.ReleaseLine, error) {
	return readProjectScopedAggregate(s, ctx, id, meta, projectActionReleaseLineRead, projectAggregateReleaseLine, s.repository.GetReleaseLine, releaseLineProjectID)
}

// ListReleaseLines returns release lines matching filter.
func (s *Service) ListReleaseLines(ctx context.Context, input ListReleaseLinesInput) (ListReleaseLinesResult, error) {
	return listProjectScoped(s, ctx, input.ProjectID, input.Meta, projectActionReleaseLineRead, projectAggregateReleaseLine,
		func(ctx context.Context) ([]entity.ReleaseLine, value.PageResult, error) {
			return s.repository.ListReleaseLines(ctx, query.ReleaseLineFilter{
				ProjectID:       input.ProjectID,
				ReleasePolicyID: input.ReleasePolicyID,
				Statuses:        input.Statuses,
				Page:            input.Page,
			})
		},
		func(lines []entity.ReleaseLine, page value.PageResult) ListReleaseLinesResult {
			return ListReleaseLinesResult{ReleaseLines: lines, Page: page}
		},
	)
}

// PutPlacementPolicy creates or updates placement policy.
func (s *Service) PutPlacementPolicy(ctx context.Context, input PutPlacementPolicyInput) (entity.PlacementPolicy, error) {
	id, previous, err := putIdentity(input.PlacementPolicyID, input.Meta, s.ids.New())
	if err != nil {
		return entity.PlacementPolicy{}, err
	}
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.PlacementPolicy{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionPlacementPolicyUpdate, projectScopedResource(projectAggregatePlacementPolicy, input.ProjectID)); err != nil {
		return entity.PlacementPolicy{}, err
	}
	if replay, ok, err := findScopedCommandReplay(s, ctx, input.Meta, projectOperationPutPlacementPolicy, projectAggregatePlacementPolicy, input.ProjectID, s.repository.GetPlacementPolicy, placementPolicyProjectID); ok || err != nil {
		if err != nil {
			return entity.PlacementPolicy{}, err
		}
		return replay, nil
	}
	now := s.clock.Now()
	policy := entity.PlacementPolicy{
		Base:               newBase(id, now),
		ProjectID:          input.ProjectID,
		RepositoryID:       input.RepositoryID,
		ServiceKey:         strings.TrimSpace(input.ServiceKey),
		AllowedClusterRefs: trimStrings(input.AllowedClusterRefs),
		Status:             defaultPlacementPolicyStatus(input.Status),
	}
	if previous != nil {
		policy.Version = *previous + 1
	}
	if len(policy.AllowedClusterRefs) == 0 {
		return entity.PlacementPolicy{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(input.Meta, projectOperationPutPlacementPolicy, projectAggregatePlacementPolicy, policy.ID, now)
	if err != nil {
		return entity.PlacementPolicy{}, err
	}
	event, err := s.placementPolicyEvent(placementPolicyEventType(previous == nil, policy.Status), policy)
	if err != nil {
		return entity.PlacementPolicy{}, err
	}
	if err := s.repository.PutPlacementPolicy(ctx, policy, previous, event, result); err != nil {
		return entity.PlacementPolicy{}, err
	}
	return policy, nil
}

// GetPlacementPolicy returns placement policy by id.
func (s *Service) GetPlacementPolicy(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.PlacementPolicy, error) {
	return readProjectScopedAggregate(s, ctx, id, meta, projectActionPlacementPolicyRead, projectAggregatePlacementPolicy, s.repository.GetPlacementPolicy, placementPolicyProjectID)
}

// ListPlacementPolicies returns placement policies matching filter.
func (s *Service) ListPlacementPolicies(ctx context.Context, input ListPlacementPoliciesInput) (ListPlacementPoliciesResult, error) {
	return listProjectScoped(s, ctx, input.ProjectID, input.Meta, projectActionPlacementPolicyRead, projectAggregatePlacementPolicy,
		func(ctx context.Context) ([]entity.PlacementPolicy, value.PageResult, error) {
			return s.repository.ListPlacementPolicies(ctx, query.PlacementPolicyFilter{
				ProjectID:    input.ProjectID,
				RepositoryID: input.RepositoryID,
				ServiceKey:   input.ServiceKey,
				Statuses:     input.Statuses,
				Page:         input.Page,
			})
		},
		func(policies []entity.PlacementPolicy, page value.PageResult) ListPlacementPoliciesResult {
			return ListPlacementPoliciesResult{PlacementPolicies: policies, Page: page}
		},
	)
}

func putIdentity(id *uuid.UUID, meta value.CommandMeta, generated uuid.UUID) (uuid.UUID, *int64, error) {
	previous, err := previousVersion(meta)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if id != nil {
		return *id, previous, nil
	}
	if previous != nil {
		return uuid.Nil, nil, errs.ErrInvalidArgument
	}
	return generated, nil, nil
}

func trimStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func branchRulesEventType(created bool, status enum.BranchRulesStatus) string {
	if status == enum.BranchRulesStatusDisabled {
		return projectEventBranchRulesDisabled
	}
	if created {
		return projectEventBranchRulesCreated
	}
	return projectEventBranchRulesUpdated
}

func releasePolicyEventType(created bool, status enum.ReleasePolicyStatus) string {
	if status == enum.ReleasePolicyStatusArchived {
		return projectEventReleasePolicyArchived
	}
	if status == enum.ReleasePolicyStatusDisabled {
		return projectEventReleasePolicyDisabled
	}
	if created {
		return projectEventReleasePolicyCreated
	}
	return projectEventReleasePolicyUpdated
}

func releaseLineEventType(created bool, status enum.ReleasePolicyStatus) string {
	if status == enum.ReleasePolicyStatusArchived {
		return projectEventReleaseLineArchived
	}
	if status == enum.ReleasePolicyStatusDisabled {
		return projectEventReleaseLineDisabled
	}
	if created {
		return projectEventReleaseLineCreated
	}
	return projectEventReleaseLineUpdated
}

func placementPolicyEventType(created bool, status enum.PlacementPolicyStatus) string {
	if status == enum.PlacementPolicyStatusDisabled {
		return projectEventPlacementPolicyDisabled
	}
	if created {
		return projectEventPlacementPolicyCreated
	}
	return projectEventPlacementPolicyUpdated
}

func (s *Service) branchRulesEvent(eventType string, rules entity.BranchRules) (entity.OutboxEvent, error) {
	return s.policyStatusEvent(eventType, projectAggregateBranchRules, rules.ID, rules.ProjectID, rules.UpdatedAt, string(rules.Status), rules.Version, payloadField(projectPayloadBranchRulesID, rules.ID.String()))
}

func (s *Service) releasePolicyEvent(eventType string, policy entity.ReleasePolicy) (entity.OutboxEvent, error) {
	return s.policyStatusEvent(eventType, projectAggregateReleasePolicy, policy.ID, policy.ProjectID, policy.UpdatedAt, string(policy.Status), policy.Version, payloadField(projectPayloadReleasePolicyID, policy.ID.String()))
}

func (s *Service) releaseLineEvent(eventType string, line entity.ReleaseLine) (entity.OutboxEvent, error) {
	return s.aggregateEvent(
		eventType,
		projectAggregateReleaseLine,
		line.ID,
		line.UpdatedAt,
		payloadProjectID(line.ProjectID),
		payloadField(projectPayloadReleasePolicyID, line.ReleasePolicyID.String()),
		payloadField(projectPayloadReleaseLineID, line.ID.String()),
		payloadField(projectPayloadStatus, string(line.Status)),
		payloadVersion(line.Version),
	)
}

func (s *Service) placementPolicyEvent(eventType string, policy entity.PlacementPolicy) (entity.OutboxEvent, error) {
	return s.policyStatusEvent(eventType, projectAggregatePlacementPolicy, policy.ID, policy.ProjectID, policy.UpdatedAt, string(policy.Status), policy.Version, payloadField(projectPayloadPlacementPolicyID, policy.ID.String()))
}

func (s *Service) policyStatusEvent(
	eventType string,
	aggregateType string,
	aggregateID uuid.UUID,
	projectID uuid.UUID,
	occurredAt time.Time,
	status string,
	version int64,
	idOption projectEventPayloadOption,
) (entity.OutboxEvent, error) {
	return s.aggregateEvent(
		eventType,
		aggregateType,
		aggregateID,
		occurredAt,
		payloadProjectID(projectID),
		idOption,
		payloadField(projectPayloadStatus, status),
		payloadVersion(version),
	)
}
