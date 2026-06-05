package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

var selfDeployBuildRuntimeCommandNamespace = uuid.MustParse("1ed56726-8301-5c31-a00f-99d489de3bd1")

func (s *Service) dispatchSelfDeployBuildIfApproved(ctx context.Context, plan entity.SelfDeployPlan) (entity.SelfDeployPlan, error) {
	if !s.selfDeployBuildDispatchEnabled {
		return plan, nil
	}
	if err := s.requireRepository(); err != nil {
		return entity.SelfDeployPlan{}, err
	}
	current, err := s.repository.GetSelfDeployPlan(ctx, plan.ID)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	if current.Status != enum.SelfDeployPlanStatusApproved {
		return current, nil
	}
	if selfDeployBuildJobsRequested(current) {
		return current, nil
	}
	if !selfDeployPlanExpectsBuild(current) {
		return s.recordSelfDeployBuildBlocked(ctx, current, "build_not_expected", "self-deploy plan does not request runtime build jobs", "")
	}
	if strings.TrimSpace(current.GovernanceContext.GateDecisionRef) == "" {
		return s.recordSelfDeployBuildBlocked(ctx, current, "governance_gate_not_approved", "self-deploy build dispatch requires an approved governance gate decision ref", "")
	}
	lookup, err := selfDeployBuildPlanLookupInput(current)
	if err != nil {
		return s.recordSelfDeployBuildBlocked(ctx, current, "invalid_self_deploy_plan_refs", "self-deploy plan refs are not valid for build plan lookup", "")
	}
	read, err := s.selfDeployBuildPlanReader.GetSelfDeployBuildPlan(ctx, lookup)
	if err != nil {
		return s.recordSelfDeployBuildFailure(ctx, current, classifyRuntimeJobFailure(err), "")
	}
	if read.Status != SelfDeployBuildPlanStatusReady {
		return s.recordSelfDeployBuildBlocked(ctx, current, string(read.Status), selfDeployBuildPlanBlockedSummary(read), read.Plan.PlanFingerprint)
	}
	if err := validateSelfDeployBuildPlan(current, read.Plan); err != nil {
		return s.recordSelfDeployBuildBlocked(ctx, current, "build_plan_conflict", "project-catalog returned a build plan that does not match the approved self-deploy plan", read.Plan.PlanFingerprint)
	}
	jobs, err := s.createSelfDeployBuildJobs(ctx, current, read.Plan)
	if err != nil {
		return s.recordSelfDeployBuildFailure(ctx, current, classifyRuntimeJobFailure(err), read.Plan.PlanFingerprint)
	}
	return s.recordSelfDeployBuildRequested(ctx, current, read.Plan, jobs)
}

func selfDeployBuildJobsRequested(plan entity.SelfDeployPlan) bool {
	return plan.RuntimeBuildStatus == enum.SelfDeployRuntimeBuildStatusRequested && len(plan.RuntimeBuildJobs) > 0
}

func selfDeployPlanExpectsBuild(plan entity.SelfDeployPlan) bool {
	for _, jobType := range plan.ExpectedRuntimeJobTypes {
		if jobType == enum.SelfDeployRuntimeJobTypeBuild {
			return true
		}
	}
	return false
}

func selfDeployBuildPlanLookupInput(plan entity.SelfDeployPlan) (SelfDeployBuildPlanLookupInput, error) {
	projectID, err := uuid.Parse(strings.TrimSpace(plan.ProjectRef))
	if err != nil || projectID == uuid.Nil {
		return SelfDeployBuildPlanLookupInput{}, errs.ErrInvalidArgument
	}
	repositoryID, err := uuid.Parse(strings.TrimSpace(plan.RepositoryRef))
	if err != nil || repositoryID == uuid.Nil {
		return SelfDeployBuildPlanLookupInput{}, errs.ErrInvalidArgument
	}
	return SelfDeployBuildPlanLookupInput{
		Meta:                         selfDeployBuildCommandMeta(plan.ID),
		ProjectID:                    projectID,
		RepositoryID:                 repositoryID,
		SourceRef:                    plan.SourceRef,
		MergeCommitSHA:               plan.MergeCommitSHA,
		ProviderSignalRef:            plan.ProviderSignalRef,
		AffectedServiceKeys:          append([]string(nil), plan.AffectedServiceKeys...),
		ExpectedServicesPolicyDigest: plan.ServicesYAMLDigest,
	}, nil
}

func validateSelfDeployBuildPlan(plan entity.SelfDeployPlan, buildPlan SelfDeployBuildPlan) error {
	planFingerprint, err := normalizeSHA256Digest(buildPlan.PlanFingerprint)
	if err != nil || planFingerprint == "" || len(buildPlan.BuildItems) == 0 {
		return errs.ErrDependencyUnavailable
	}
	if !selfDeployBuildPlanMatchesApprovedPlan(plan, buildPlan) {
		return errs.ErrConflict
	}
	for _, item := range buildPlan.BuildItems {
		itemFingerprint, err := normalizeSHA256Digest(item.PlanItemFingerprint)
		if err != nil || itemFingerprint == "" {
			return errs.ErrDependencyUnavailable
		}
		if strings.TrimSpace(item.ServiceKey) == "" ||
			strings.TrimSpace(item.ServiceKey) != strings.TrimSpace(item.BuildExecutionSpec.ServiceKey) ||
			strings.TrimSpace(item.BuildExecutionSpec.BuildPlanFingerprint) != planFingerprint {
			return errs.ErrDependencyUnavailable
		}
	}
	return nil
}

func (s *Service) createSelfDeployBuildJobs(ctx context.Context, plan entity.SelfDeployPlan, buildPlan SelfDeployBuildPlan) ([]entity.SelfDeployRuntimeBuildJob, error) {
	lookup, err := selfDeployBuildPlanLookupInput(plan)
	if err != nil {
		return nil, err
	}
	jobs := make([]entity.SelfDeployRuntimeBuildJob, 0, len(buildPlan.BuildItems))
	for _, item := range buildPlan.BuildItems {
		result, err := s.selfDeployBuildJobCreator.CreateSelfDeployBuildJob(ctx, SelfDeployBuildJobInput{
			Meta:                  selfDeployRuntimeBuildCommandMeta(plan.ID, item),
			ProjectID:             lookup.ProjectID,
			RepositoryID:          lookup.RepositoryID,
			PlanID:                plan.ID,
			ServiceKey:            item.ServiceKey,
			ServiceRef:            item.ServiceRef,
			PlanFingerprint:       buildPlan.PlanFingerprint,
			PlanItemFingerprint:   item.PlanItemFingerprint,
			BuildExecutionSpec:    item.BuildExecutionSpec,
			GovernanceApprovalRef: plan.GovernanceContext.GateDecisionRef,
			GovernanceGateRef:     plan.GovernanceContext.GateRequestRef,
		})
		if err != nil {
			return nil, err
		}
		jobRef, err := normalizeSelfDeployRef(result.JobRef, true)
		if err != nil {
			return nil, selfDeployRuntimeRefError()
		}
		serviceRef, err := normalizeSelfDeployRef(item.ServiceRef, false)
		if err != nil {
			return nil, selfDeployRuntimeRefError()
		}
		jobs = append(jobs, entity.SelfDeployRuntimeBuildJob{
			ServiceKey:               item.ServiceKey,
			ServiceRef:               serviceRef,
			RuntimeJobRef:            jobRef,
			RuntimeJobStatus:         result.Status,
			BuildPlanItemFingerprint: item.PlanItemFingerprint,
		})
	}
	return jobs, nil
}

func selfDeployRuntimeRefError() error {
	return NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager returned unsafe build job refs")
}

func (s *Service) recordSelfDeployBuildRequested(ctx context.Context, plan entity.SelfDeployPlan, buildPlan SelfDeployBuildPlan, jobs []entity.SelfDeployRuntimeBuildJob) (entity.SelfDeployPlan, error) {
	plan.RuntimeBuildJobs = append([]entity.SelfDeployRuntimeBuildJob(nil), jobs...)
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusRequested
	plan.RuntimeBuildFingerprint = strings.TrimSpace(buildPlan.PlanFingerprint)
	plan.RuntimeBuildErrorCode = ""
	plan.RuntimeBuildSummary = selfDeploySafeSummary("self-deploy build jobs requested")
	return s.recordSelfDeployBuildState(ctx, plan)
}

func (s *Service) recordSelfDeployBuildBlocked(ctx context.Context, plan entity.SelfDeployPlan, code string, summary string, fingerprint string) (entity.SelfDeployPlan, error) {
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusBlocked
	plan.RuntimeBuildFingerprint = strings.TrimSpace(fingerprint)
	plan.RuntimeBuildErrorCode = selfDeploySafeSummary(code)
	plan.RuntimeBuildSummary = selfDeploySafeSummary(summary)
	return s.recordSelfDeployBuildState(ctx, plan)
}

func (s *Service) recordSelfDeployBuildFailure(ctx context.Context, plan entity.SelfDeployPlan, failure runtimeOperationFailure, fingerprint string) (entity.SelfDeployPlan, error) {
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusFailed
	plan.RuntimeBuildFingerprint = strings.TrimSpace(fingerprint)
	plan.RuntimeBuildErrorCode = selfDeploySafeSummary(failure.code)
	plan.RuntimeBuildSummary = selfDeploySafeSummary(failure.summary())
	return s.recordSelfDeployBuildState(ctx, plan)
}

func (s *Service) recordSelfDeployBuildState(ctx context.Context, plan entity.SelfDeployPlan) (entity.SelfDeployPlan, error) {
	loaded, err := s.repository.GetSelfDeployPlan(ctx, plan.ID)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	if sameSelfDeployRuntimeBuildState(loaded, plan) {
		return loaded, nil
	}
	if loaded.Version != plan.Version {
		if selfDeployBuildJobsRequested(loaded) {
			return loaded, nil
		}
		return entity.SelfDeployPlan{}, errs.ErrConflict
	}
	now := s.clock.Now()
	previousVersion := plan.Version
	plan.Version++
	plan.UpdatedAt = now
	payload, err := marshalCommandPayload(selfDeployPlanCommandPayload{SelfDeployPlan: plan})
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	command, err := commandResult(selfDeployBuildStateCommandMeta(plan), operationDispatchSelfDeployBuild, enum.CommandAggregateTypeSelfDeployPlan, plan.ID, payload, now)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	event, err := selfDeployPlanRequestedEvent(s.idGenerator.New(), plan, now)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	if err := s.repository.UpdateSelfDeployPlanWithResult(ctx, plan, previousVersion, command, &event); err != nil {
		return s.resolveSelfDeployBuildUpdateError(ctx, plan, err)
	}
	return plan, nil
}

func selfDeployBuildPlanMatchesApprovedPlan(plan entity.SelfDeployPlan, buildPlan SelfDeployBuildPlan) bool {
	if strings.TrimSpace(buildPlan.ProjectRef) != strings.TrimSpace(plan.ProjectRef) {
		return false
	}
	if strings.TrimSpace(buildPlan.RepositoryRef) != strings.TrimSpace(plan.RepositoryRef) {
		return false
	}
	if strings.TrimSpace(buildPlan.SourceRef) != strings.TrimSpace(plan.SourceRef) {
		return false
	}
	if strings.TrimSpace(buildPlan.MergeCommitSHA) != strings.TrimSpace(plan.MergeCommitSHA) {
		return false
	}
	if strings.TrimSpace(buildPlan.ServicesYAML.Digest) != strings.TrimSpace(plan.ServicesYAMLDigest) {
		return false
	}
	return stringSlicesSetEqual(buildPlan.AffectedServiceKeys, plan.AffectedServiceKeys)
}

func (s *Service) resolveSelfDeployBuildUpdateError(ctx context.Context, desired entity.SelfDeployPlan, updateErr error) (entity.SelfDeployPlan, error) {
	loaded, loadErr := s.repository.GetSelfDeployPlan(ctx, desired.ID)
	if loadErr == nil && (selfDeployBuildJobsRequested(loaded) || sameSelfDeployRuntimeBuildState(loaded, desired)) {
		return loaded, nil
	}
	return entity.SelfDeployPlan{}, updateErr
}

func selfDeployBuildPlanBlockedSummary(read SelfDeployBuildPlanReadResult) string {
	if strings.TrimSpace(read.SafeReason) != "" {
		return "self-deploy build plan is not ready: " + strings.TrimSpace(read.SafeReason)
	}
	return "self-deploy build plan is not ready"
}

func selfDeployBuildCommandMeta(planID uuid.UUID) value.CommandMeta {
	return value.CommandMeta{
		IdempotencyKey: "self_deploy_build:" + planID.String(),
		Actor:          value.Actor{Type: "service", ID: "agent-manager"},
	}
}

func selfDeployBuildStateCommandMeta(plan entity.SelfDeployPlan) value.CommandMeta {
	return value.CommandMeta{
		IdempotencyKey: strings.Join([]string{
			"self_deploy_build",
			plan.ID.String(),
			string(plan.RuntimeBuildStatus),
			selfDeployBuildKeyDigest(plan.RuntimeBuildFingerprint),
			selfDeployBuildKeyDigest(plan.RuntimeBuildErrorCode),
			selfDeployBuildKeyDigest(plan.RuntimeBuildSummary),
		}, ":"),
		Actor: value.Actor{Type: "service", ID: "agent-manager"},
	}
}

func selfDeployBuildKeyDigest(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:])
}

func selfDeployRuntimeBuildCommandMeta(planID uuid.UUID, item SelfDeployBuildPlanItem) value.CommandMeta {
	serviceKey := strings.TrimSpace(item.ServiceKey)
	fingerprint := strings.TrimSpace(item.PlanItemFingerprint)
	return value.CommandMeta{
		CommandID:      uuid.NewSHA1(selfDeployBuildRuntimeCommandNamespace, []byte("runtime-build:"+planID.String()+":"+serviceKey+":"+fingerprint)),
		IdempotencyKey: "self_deploy_build_job:" + planID.String() + ":" + serviceKey,
		Actor:          value.Actor{Type: "service", ID: "agent-manager"},
	}
}

func selfDeploySafeSummary(value string) string {
	result, err := normalizeSelfDeployText(value, selfDeploySummaryLimit, false)
	if err != nil {
		return "self-deploy build dispatch diagnostic redacted"
	}
	return result
}

func sameSelfDeployRuntimeBuildState(left entity.SelfDeployPlan, right entity.SelfDeployPlan) bool {
	return left.RuntimeBuildStatus == right.RuntimeBuildStatus &&
		strings.TrimSpace(left.RuntimeBuildFingerprint) == strings.TrimSpace(right.RuntimeBuildFingerprint) &&
		strings.TrimSpace(left.RuntimeBuildErrorCode) == strings.TrimSpace(right.RuntimeBuildErrorCode) &&
		strings.TrimSpace(left.RuntimeBuildSummary) == strings.TrimSpace(right.RuntimeBuildSummary) &&
		sameSelfDeployRuntimeBuildJobs(left.RuntimeBuildJobs, right.RuntimeBuildJobs)
}

func sameSelfDeployRuntimeBuildJobs(left []entity.SelfDeployRuntimeBuildJob, right []entity.SelfDeployRuntimeBuildJob) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if strings.TrimSpace(left[index].ServiceKey) != strings.TrimSpace(right[index].ServiceKey) ||
			strings.TrimSpace(left[index].ServiceRef) != strings.TrimSpace(right[index].ServiceRef) ||
			strings.TrimSpace(left[index].RuntimeJobRef) != strings.TrimSpace(right[index].RuntimeJobRef) ||
			strings.TrimSpace(left[index].RuntimeJobStatus) != strings.TrimSpace(right[index].RuntimeJobStatus) ||
			strings.TrimSpace(left[index].BuildPlanItemFingerprint) != strings.TrimSpace(right[index].BuildPlanItemFingerprint) {
			return false
		}
	}
	return true
}

func stringSlicesSetEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[strings.TrimSpace(value)]++
	}
	for _, value := range right {
		key := strings.TrimSpace(value)
		counts[key]--
		if counts[key] < 0 {
			return false
		}
	}
	return true
}
