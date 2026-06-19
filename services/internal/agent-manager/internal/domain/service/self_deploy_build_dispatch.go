package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

var (
	selfDeployBuildRuntimeCommandNamespace   = uuid.MustParse("1ed56726-8301-5c31-a00f-99d489de3bd1")
	selfDeployContextRuntimeCommandNamespace = uuid.MustParse("b3f3e4a8-7009-5a10-9e6a-f6bc4cd3d4f1")
	selfDeployDeployRuntimeCommandNamespace  = uuid.MustParse("5b13dc64-f7ec-578e-bf8f-7c4fef908094")
)

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
	current = withSelfDeployRetryableRuntimeRecovery(current)
	if selfDeployBuildJobsSucceeded(current) {
		return s.dispatchSelfDeployDeployIfBuildSucceeded(ctx, current)
	}
	if selfDeployBuildJobsRequested(current) {
		return s.observeSelfDeployBuildJobs(ctx, current)
	}
	if selfDeployBuildRuntimeBlockerIsTerminal(current) {
		return s.recordSelfDeployTerminalBlocker(ctx, current, current.RuntimeBuildErrorCode, current.RuntimeBuildSummary, current.RuntimeBuildFingerprint, string(SelfDeployBuildPlanStatusPolicyStale), s.recordSelfDeployBuildBlocked)
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
	if selfDeployBuildPlanNeedsContext(read.Status) {
		contexts, contextErr := s.prepareSelfDeployBuildContexts(ctx, current, read.Plan)
		if contextErr != nil {
			return s.recordSelfDeployBuildFailure(ctx, current, classifyRuntimeJobFailure(contextErr), read.Plan.PlanFingerprint)
		}
		current.RuntimeBuildContexts = contexts
		if !selfDeployRuntimeBuildContextsReady(contexts) {
			return s.recordSelfDeployBuildPreparingContext(ctx, current, read.Status, selfDeployBuildPlanBlockedSummary(read), read.Plan.PlanFingerprint)
		}
		lookup.MaterializedBuildContexts = selfDeployMaterializedContextsFromRuntime(contexts)
		read, err = s.selfDeployBuildPlanReader.GetSelfDeployBuildPlan(ctx, lookup)
		if err != nil {
			return s.recordSelfDeployBuildFailure(ctx, current, classifyRuntimeJobFailure(err), read.Plan.PlanFingerprint)
		}
	}
	if read.Status != SelfDeployBuildPlanStatusReady {
		return s.recordSelfDeployBuildBlocked(ctx, current, string(read.Status), selfDeployBuildPlanBlockedSummary(read), read.Plan.PlanFingerprint)
	}
	if err := validateSelfDeployBuildPlan(current, read.Plan); err != nil {
		if errors.Is(err, errs.ErrDependencyUnavailable) || errors.Is(err, errs.ErrInvalidArgument) {
			return s.recordSelfDeployBuildBlocked(ctx, current, "invalid_build_execution_spec", "project-catalog returned a ready build plan without safe runtime build context refs or digests", read.Plan.PlanFingerprint)
		}
		return s.recordSelfDeployBuildBlocked(ctx, current, "build_plan_conflict", "project-catalog returned a build plan that does not match the approved self-deploy plan", read.Plan.PlanFingerprint)
	}
	jobs, err := s.createSelfDeployBuildJobs(ctx, current, read.Plan)
	if err != nil {
		return s.recordSelfDeployBuildFailure(ctx, current, classifyRuntimeJobFailure(err), read.Plan.PlanFingerprint)
	}
	return s.recordSelfDeployBuildRequested(ctx, current, read.Plan, jobs)
}

func (s *Service) dispatchSelfDeployDeployIfBuildSucceeded(ctx context.Context, plan entity.SelfDeployPlan) (entity.SelfDeployPlan, error) {
	if !s.selfDeployBuildDispatchEnabled {
		return plan, nil
	}
	if !selfDeployPlanExpectsDeploy(plan) {
		return plan, nil
	}
	if selfDeployDeployJobsSucceeded(plan) {
		return plan, nil
	}
	if selfDeployDeployJobsRequested(plan) {
		return s.observeSelfDeployDeployJobs(ctx, plan)
	}
	if !selfDeployBuildJobsSucceeded(plan) {
		return s.recordSelfDeployDeployBlocked(ctx, plan, "build_not_succeeded", "self-deploy deploy waits for successful build jobs", "")
	}
	plan = withSelfDeployRetryableDeployRuntimeRecovery(plan)
	if selfDeployDeployRuntimeBlockerIsTerminal(plan) {
		return s.recordSelfDeployTerminalBlocker(ctx, plan, plan.RuntimeDeployErrorCode, plan.RuntimeDeploySummary, plan.RuntimeDeployFingerprint, string(SelfDeployDeployPlanStatusPolicyStale), s.recordSelfDeployDeployBlocked)
	}
	lookup, err := selfDeployBuildPlanLookupInput(plan)
	if err != nil {
		return s.recordSelfDeployDeployBlocked(ctx, plan, "invalid_self_deploy_plan_refs", "self-deploy plan refs are not valid for deploy plan lookup", "")
	}
	buildRead, err := s.selfDeployBuildPlanReader.GetSelfDeployBuildPlan(ctx, selfDeployBuildPlanLookupWithContexts(lookup, plan))
	if err != nil {
		return s.recordSelfDeployDeployFailure(ctx, plan, classifyRuntimeJobFailure(err), plan.RuntimeBuildFingerprint)
	}
	if buildRead.Status != SelfDeployBuildPlanStatusReady {
		return s.recordSelfDeployDeployBlocked(ctx, plan, string(buildRead.Status), selfDeployBuildPlanBlockedSummary(buildRead), buildRead.Plan.PlanFingerprint)
	}
	if err := validateSelfDeployBuildPlan(plan, buildRead.Plan); err != nil {
		return s.recordSelfDeployDeployBlocked(ctx, plan, "build_plan_conflict", "project-catalog returned a build plan that does not match the approved self-deploy plan", buildRead.Plan.PlanFingerprint)
	}
	buildOutputs, err := selfDeployBuildOutputsFromPlan(plan, buildRead.Plan)
	if err != nil {
		return s.recordSelfDeployDeployBlocked(ctx, plan, "build_output_invalid", "self-deploy build outputs are not safe for deploy planning", buildRead.Plan.PlanFingerprint)
	}
	deployRead, err := s.selfDeployDeployPlanReader.GetSelfDeployDeployPlan(ctx, selfDeployDeployPlanLookupInput(lookup, buildRead.Plan, buildOutputs, plan))
	if err != nil {
		return s.recordSelfDeployDeployFailure(ctx, plan, classifyRuntimeJobFailure(err), buildRead.Plan.PlanFingerprint)
	}
	if deployRead.Status != SelfDeployDeployPlanStatusReady {
		return s.recordSelfDeployDeployBlocked(ctx, plan, string(deployRead.Status), selfDeployDeployPlanBlockedSummary(deployRead), deployRead.Plan.PlanFingerprint)
	}
	if err := validateSelfDeployDeployPlan(plan, deployRead.Plan); err != nil {
		return s.recordSelfDeployDeployBlocked(ctx, plan, "invalid_deploy_execution_spec", "project-catalog returned a deploy plan without safe runtime deploy refs or digests", deployRead.Plan.PlanFingerprint)
	}
	jobs, err := s.createSelfDeployDeployJobs(ctx, plan, deployRead.Plan)
	if err != nil {
		return s.recordSelfDeployDeployFailure(ctx, plan, classifyRuntimeJobFailure(err), deployRead.Plan.PlanFingerprint)
	}
	return s.recordSelfDeployDeployRequested(ctx, plan, deployRead.Plan, jobs)
}

func selfDeployBuildJobsRequested(plan entity.SelfDeployPlan) bool {
	return plan.RuntimeBuildStatus == enum.SelfDeployRuntimeBuildStatusRequested && len(plan.RuntimeBuildJobs) > 0
}

func selfDeployBuildJobsSucceeded(plan entity.SelfDeployPlan) bool {
	return plan.RuntimeBuildStatus == enum.SelfDeployRuntimeBuildStatusSucceeded && len(plan.RuntimeBuildJobs) > 0
}

func selfDeployDeployJobsRequested(plan entity.SelfDeployPlan) bool {
	return plan.RuntimeDeployStatus == enum.SelfDeployRuntimeDeployStatusRequested && len(plan.RuntimeDeployJobs) > 0
}

func selfDeployDeployJobsSucceeded(plan entity.SelfDeployPlan) bool {
	return plan.RuntimeDeployStatus == enum.SelfDeployRuntimeDeployStatusSucceeded && len(plan.RuntimeDeployJobs) > 0
}

func (s *Service) observeSelfDeployBuildJobs(ctx context.Context, plan entity.SelfDeployPlan) (entity.SelfDeployPlan, error) {
	updated := plan
	allSucceeded, terminal, err := s.refreshSelfDeployRuntimeJobs(ctx, len(updated.RuntimeBuildJobs), enum.SelfDeployRuntimeJobTypeBuild, func(index int) string {
		return updated.RuntimeBuildJobs[index].RuntimeJobRef
	}, func(index int, status string) {
		updated.RuntimeBuildJobs[index].RuntimeJobStatus = status
	})
	if err != nil {
		return s.recordSelfDeployBuildFailure(ctx, updated, classifyRuntimeJobFailure(err), updated.RuntimeBuildFingerprint)
	}
	if terminal != nil {
		return s.recordSelfDeployBuildDiagnostic(ctx, updated, enum.SelfDeployRuntimeBuildStatusFailed, firstNonEmpty(terminal.SafeErrorCode, string(terminal.Status)), firstNonEmpty(terminal.SafeErrorSummary, terminal.SafeSummary), updated.RuntimeBuildFingerprint)
	}
	if !allSucceeded {
		return s.recordSelfDeployBuildState(ctx, updated)
	}
	recorded, err := s.recordSelfDeployBuildSucceeded(ctx, updated)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	return s.dispatchSelfDeployDeployIfBuildSucceeded(ctx, recorded)
}

func (s *Service) observeSelfDeployDeployJobs(ctx context.Context, plan entity.SelfDeployPlan) (entity.SelfDeployPlan, error) {
	updated := plan
	allSucceeded, terminal, err := s.refreshSelfDeployRuntimeJobs(ctx, len(updated.RuntimeDeployJobs), enum.SelfDeployRuntimeJobTypeDeploy, deployRuntimeJobRefAt(updated.RuntimeDeployJobs), setDeployRuntimeJobStatus(updated.RuntimeDeployJobs))
	if err != nil {
		return s.recordSelfDeployDeployFailure(ctx, updated, classifyRuntimeJobFailure(err), updated.RuntimeDeployFingerprint)
	}
	if terminal != nil {
		return s.recordSelfDeployDeployDiagnostic(ctx, updated, enum.SelfDeployRuntimeDeployStatusFailed, firstNonEmpty(terminal.SafeErrorCode, string(terminal.Status)), firstNonEmpty(terminal.SafeErrorSummary, terminal.SafeSummary), updated.RuntimeDeployFingerprint)
	}
	if !allSucceeded {
		return s.recordSelfDeployDeployState(ctx, updated)
	}
	return s.recordSelfDeployDeploySucceeded(ctx, updated)
}

func deployRuntimeJobRefAt(jobs []entity.SelfDeployRuntimeDeployJob) func(int) string {
	return func(index int) string {
		return jobs[index].RuntimeJobRef
	}
}

func setDeployRuntimeJobStatus(jobs []entity.SelfDeployRuntimeDeployJob) func(int, string) {
	return func(index int, status string) {
		jobs[index].RuntimeJobStatus = status
	}
}

func (s *Service) refreshSelfDeployRuntimeJobs(ctx context.Context, jobCount int, jobType enum.SelfDeployRuntimeJobType, jobRefAt func(int) string, setStatusAt func(int, string)) (bool, *SelfDeployRuntimeJobReadResult, error) {
	allSucceeded := jobCount > 0
	for index := 0; index < jobCount; index++ {
		read, err := s.readSelfDeployRuntimeJob(ctx, jobRefAt(index), jobType)
		if err != nil {
			return false, nil, err
		}
		setStatusAt(index, string(read.Status))
		if read.Status != RuntimeJobStatusSucceeded {
			allSucceeded = false
		}
		if selfDeployRuntimeJobTerminalFailure(read.Status) {
			return false, &read, nil
		}
	}
	return allSucceeded, nil, nil
}

func (s *Service) readSelfDeployRuntimeJob(ctx context.Context, jobRef string, jobType enum.SelfDeployRuntimeJobType) (SelfDeployRuntimeJobReadResult, error) {
	return s.selfDeployRuntimeJobReader.GetSelfDeployRuntimeJob(ctx, SelfDeployRuntimeJobReadInput{
		Meta:    selfDeployRuntimeJobQueryMeta(),
		JobRef:  jobRef,
		JobType: jobType,
	})
}

func selfDeployRuntimeJobTerminalFailure(status RuntimeJobStatus) bool {
	switch status {
	case RuntimeJobStatusFailed, RuntimeJobStatusCancelled, RuntimeJobStatusTimedOut:
		return true
	default:
		return false
	}
}

// SelfDeployPlanNeedsRuntimeRecovery reports whether an approved plan should retry or observe runtime state.
func SelfDeployPlanNeedsRuntimeRecovery(plan entity.SelfDeployPlan) bool {
	if plan.ID == uuid.Nil ||
		plan.Status != enum.SelfDeployPlanStatusApproved ||
		strings.TrimSpace(plan.GovernanceContext.GateDecisionRef) == "" {
		return false
	}
	if selfDeployPlanExpectsBuild(plan) {
		switch plan.RuntimeBuildStatus {
		case "", enum.SelfDeployRuntimeBuildStatusNotRequested, enum.SelfDeployRuntimeBuildStatusPreparingContext, enum.SelfDeployRuntimeBuildStatusRequested:
			return true
		case enum.SelfDeployRuntimeBuildStatusBlocked, enum.SelfDeployRuntimeBuildStatusFailed:
			return selfDeployRuntimeBlockerRetryable(plan.RuntimeBuildErrorCode)
		case enum.SelfDeployRuntimeBuildStatusSucceeded:
		default:
			return false
		}
	}
	if !selfDeployPlanExpectsDeploy(plan) || !selfDeployBuildJobsSucceeded(plan) {
		return false
	}
	switch plan.RuntimeDeployStatus {
	case "", enum.SelfDeployRuntimeDeployStatusNotRequested, enum.SelfDeployRuntimeDeployStatusRequested:
		return true
	case enum.SelfDeployRuntimeDeployStatusBlocked, enum.SelfDeployRuntimeDeployStatusFailed:
		return selfDeployRuntimeBlockerRetryable(plan.RuntimeDeployErrorCode)
	default:
		return false
	}
}

func withSelfDeployRetryableRuntimeRecovery(plan entity.SelfDeployPlan) entity.SelfDeployPlan {
	if !selfDeployRuntimeBuildBlockerRetryable(plan) {
		return plan
	}
	plan.RuntimeBuildContexts = nil
	plan.RuntimeBuildJobs = nil
	plan = withSelfDeployBuildRuntimeProgress(plan, enum.SelfDeployRuntimeBuildStatusNotRequested, "", "", "self-deploy runtime build retry requested after retryable blocker")
	plan.RuntimeDeployJobs = nil
	plan = withSelfDeployDeployRuntimeProgress(plan, enum.SelfDeployRuntimeDeployStatusNotRequested, "", "", "")
	return plan
}

func withSelfDeployRetryableDeployRuntimeRecovery(plan entity.SelfDeployPlan) entity.SelfDeployPlan {
	if !selfDeployRuntimeDeployBlockerRetryable(plan) {
		return plan
	}
	plan.RuntimeDeployJobs = nil
	return withSelfDeployDeployRuntimeProgress(plan, enum.SelfDeployRuntimeDeployStatusNotRequested, "", "", "self-deploy runtime deploy retry requested after retryable blocker")
}

func selfDeployRuntimeBuildBlockerRetryable(plan entity.SelfDeployPlan) bool {
	switch plan.RuntimeBuildStatus {
	case enum.SelfDeployRuntimeBuildStatusBlocked, enum.SelfDeployRuntimeBuildStatusFailed:
		return selfDeployRuntimeBlockerRetryable(plan.RuntimeBuildErrorCode)
	default:
		return false
	}
}

func selfDeployRuntimeDeployBlockerRetryable(plan entity.SelfDeployPlan) bool {
	switch plan.RuntimeDeployStatus {
	case enum.SelfDeployRuntimeDeployStatusBlocked, enum.SelfDeployRuntimeDeployStatusFailed:
		return selfDeployRuntimeBlockerRetryable(plan.RuntimeDeployErrorCode)
	default:
		return false
	}
}

func selfDeployBuildRuntimeBlockerIsTerminal(plan entity.SelfDeployPlan) bool {
	switch plan.RuntimeBuildStatus {
	case enum.SelfDeployRuntimeBuildStatusBlocked, enum.SelfDeployRuntimeBuildStatusFailed:
		return !selfDeployRuntimeBlockerRetryable(plan.RuntimeBuildErrorCode)
	default:
		return false
	}
}

func selfDeployDeployRuntimeBlockerIsTerminal(plan entity.SelfDeployPlan) bool {
	switch plan.RuntimeDeployStatus {
	case enum.SelfDeployRuntimeDeployStatusBlocked, enum.SelfDeployRuntimeDeployStatusFailed:
		return !selfDeployRuntimeBlockerRetryable(plan.RuntimeDeployErrorCode)
	default:
		return false
	}
}

func selfDeployRuntimeBlockerRetryable(code string) bool {
	switch strings.TrimSpace(code) {
	case "permission_denied",
		"dependency_unavailable",
		"conflict",
		"deadline_exceeded",
		"context_deadline_exceeded",
		"context_cancelled",
		"configuration_unavailable",
		"config_not_ready",
		"materializer_not_ready",
		string(SelfDeployBuildPlanStatusBuildContextUnavailable):
		return true
	default:
		return false
	}
}

func selfDeployPlanExpectsBuild(plan entity.SelfDeployPlan) bool {
	for _, jobType := range plan.ExpectedRuntimeJobTypes {
		if jobType == enum.SelfDeployRuntimeJobTypeBuild {
			return true
		}
	}
	return false
}

func selfDeployPlanExpectsDeploy(plan entity.SelfDeployPlan) bool {
	for _, jobType := range plan.ExpectedRuntimeJobTypes {
		if jobType == enum.SelfDeployRuntimeJobTypeDeploy {
			return true
		}
	}
	return false
}

func selfDeployBuildPlanNeedsContext(status SelfDeployBuildPlanStatus) bool {
	return status == SelfDeployBuildPlanStatusBuildContextRequired ||
		status == SelfDeployBuildPlanStatusBuildContextUnavailable
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

func selfDeployBuildPlanLookupWithContexts(input SelfDeployBuildPlanLookupInput, plan entity.SelfDeployPlan) SelfDeployBuildPlanLookupInput {
	input.MaterializedBuildContexts = selfDeployMaterializedContextsFromRuntime(plan.RuntimeBuildContexts)
	input.ExpectedBuildPlanFingerprint = strings.TrimSpace(plan.RuntimeBuildFingerprint)
	return input
}

func selfDeployDeployPlanLookupInput(input SelfDeployBuildPlanLookupInput, buildPlan SelfDeployBuildPlan, outputs []SelfDeployBuildOutput, plan entity.SelfDeployPlan) SelfDeployDeployPlanLookupInput {
	return SelfDeployDeployPlanLookupInput{
		Meta:                              selfDeployDeployCommandMeta(plan.ID),
		ProjectID:                         input.ProjectID,
		RepositoryID:                      input.RepositoryID,
		SourceRef:                         input.SourceRef,
		MergeCommitSHA:                    input.MergeCommitSHA,
		ProviderSignalRef:                 input.ProviderSignalRef,
		AffectedServiceKeys:               append([]string(nil), input.AffectedServiceKeys...),
		ExpectedServicesPolicyDigest:      input.ExpectedServicesPolicyDigest,
		ExpectedServicesPolicyFingerprint: input.ExpectedServicesPolicyFingerprint,
		ExpectedServicesPolicyVersion:     input.ExpectedServicesPolicyVersion,
		ExpectedBuildPlanFingerprint:      buildPlan.PlanFingerprint,
		ExpectedDeployPlanFingerprint:     plan.RuntimeDeployFingerprint,
		BuildOutputs:                      append([]SelfDeployBuildOutput(nil), outputs...),
		MaterializedBuildContexts:         selfDeployMaterializedContextsFromRuntime(plan.RuntimeBuildContexts),
	}
}

func (s *Service) prepareSelfDeployBuildContexts(ctx context.Context, plan entity.SelfDeployPlan, buildPlan SelfDeployBuildPlan) ([]entity.SelfDeployRuntimeBuildContext, error) {
	if len(plan.RuntimeBuildContexts) > 0 {
		return s.refreshSelfDeployBuildContexts(ctx, plan.RuntimeBuildContexts)
	}
	lookup, err := selfDeployBuildPlanLookupInput(plan)
	if err != nil {
		return nil, err
	}
	result, err := s.selfDeployBuildContextPreparer.PrepareSelfDeployBuildContext(ctx, SelfDeployBuildContextInput{
		Meta:                              selfDeployBuildContextCommandMeta(plan.ID, buildPlan.PlanFingerprint),
		ProjectID:                         lookup.ProjectID,
		RepositoryID:                      lookup.RepositoryID,
		ProviderSlug:                      plan.ProviderSlug,
		RepositoryFullName:                plan.RepositoryFullName,
		SourceRef:                         plan.SourceRef,
		MergeCommitSHA:                    plan.MergeCommitSHA,
		AffectedServiceKeys:               append([]string(nil), plan.AffectedServiceKeys...),
		ExpectedBuildPlanFingerprint:      buildPlan.PlanFingerprint,
		ExpectedServicesPolicyDigest:      plan.ServicesYAMLDigest,
		ExpectedServicesPolicyFingerprint: buildPlan.ServicesYAML.Fingerprint,
	})
	if err != nil {
		return nil, err
	}
	return selfDeployRuntimeBuildContextsFromResult(buildPlan, result)
}

func (s *Service) refreshSelfDeployBuildContexts(ctx context.Context, contexts []entity.SelfDeployRuntimeBuildContext) ([]entity.SelfDeployRuntimeBuildContext, error) {
	result := append([]entity.SelfDeployRuntimeBuildContext(nil), contexts...)
	for index := range result {
		read, err := s.selfDeployBuildContextPreparer.GetSelfDeployBuildContext(ctx, SelfDeployBuildContextReadInput{
			Meta:            selfDeployRuntimeJobQueryMeta(),
			BuildContextRef: result[index].RuntimeBuildContextRef,
		})
		if err != nil {
			return nil, err
		}
		result[index].RuntimeBuildContextStatus = read.RuntimeBuildContextStatus
		result[index].BuildContextRef = read.BuildContextRef
		result[index].BuildContextDigest = read.BuildContextDigest
		result[index].ManifestBundleDigest = selfDeployManifestBundleDigestForService(read.ManifestBundleDigests, result[index].ServiceKey)
		result[index].MaterializationFingerprint = read.MaterializationFingerprint
	}
	return result, nil
}

func selfDeployRuntimeBuildContextsFromResult(buildPlan SelfDeployBuildPlan, result SelfDeployBuildContextResult) ([]entity.SelfDeployRuntimeBuildContext, error) {
	runtimeRef, err := normalizeSelfDeployRef(result.RuntimeBuildContextRef, true)
	if err != nil {
		return nil, selfDeployRuntimeRefError()
	}
	contextRef, err := normalizeSelfDeployRef(result.BuildContextRef, false)
	if err != nil {
		return nil, selfDeployRuntimeRefError()
	}
	contextDigest := strings.TrimSpace(result.BuildContextDigest)
	if contextDigest != "" {
		if _, err := normalizeSHA256Digest(contextDigest); err != nil {
			return nil, selfDeployRuntimeRefError()
		}
	}
	contexts := make([]entity.SelfDeployRuntimeBuildContext, 0, len(buildPlan.BuildItems))
	for _, item := range buildPlan.BuildItems {
		manifestBundleDigest := selfDeployManifestBundleDigestForService(result.ManifestBundleDigests, item.ServiceKey)
		if manifestBundleDigest != "" {
			if _, err := normalizeSHA256Digest(manifestBundleDigest); err != nil {
				return nil, selfDeployRuntimeRefError()
			}
		}
		contexts = append(contexts, entity.SelfDeployRuntimeBuildContext{
			ServiceKey:                 item.ServiceKey,
			RuntimeBuildContextRef:     runtimeRef,
			RuntimeBuildContextStatus:  result.RuntimeBuildContextStatus,
			BuildContextRef:            contextRef,
			BuildContextDigest:         contextDigest,
			ManifestBundleDigest:       manifestBundleDigest,
			MaterializationFingerprint: result.MaterializationFingerprint,
			BuildPlanItemFingerprint:   item.PlanItemFingerprint,
		})
	}
	return contexts, nil
}

func selfDeployRuntimeBuildContextsReady(contexts []entity.SelfDeployRuntimeBuildContext) bool {
	if len(contexts) == 0 {
		return false
	}
	for _, context := range contexts {
		if strings.TrimSpace(context.RuntimeBuildContextStatus) != "ready" ||
			strings.TrimSpace(context.BuildContextRef) == "" ||
			strings.TrimSpace(context.BuildContextDigest) == "" {
			return false
		}
	}
	return true
}

func selfDeployMaterializedContextsFromRuntime(contexts []entity.SelfDeployRuntimeBuildContext) []SelfDeployMaterializedBuildContext {
	result := make([]SelfDeployMaterializedBuildContext, 0, len(contexts))
	for _, context := range contexts {
		if strings.TrimSpace(context.BuildContextRef) == "" || strings.TrimSpace(context.BuildContextDigest) == "" {
			continue
		}
		result = append(result, SelfDeployMaterializedBuildContext{
			ServiceKey:                 context.ServiceKey,
			PlanItemFingerprint:        context.BuildPlanItemFingerprint,
			BuildContextRef:            context.BuildContextRef,
			BuildContextDigest:         context.BuildContextDigest,
			DockerfileDigest:           context.DockerfileDigest,
			MaterializationRef:         context.RuntimeBuildContextRef,
			MaterializationFingerprint: context.MaterializationFingerprint,
			ManifestBundleDigest:       context.ManifestBundleDigest,
		})
	}
	return result
}

func selfDeployManifestBundleDigestForService(values map[string]string, serviceKey string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(values[strings.TrimSpace(serviceKey)]))
}

func selfDeployBuildOutputsFromPlan(plan entity.SelfDeployPlan, buildPlan SelfDeployBuildPlan) ([]SelfDeployBuildOutput, error) {
	jobs := make(map[string]entity.SelfDeployRuntimeBuildJob, len(plan.RuntimeBuildJobs))
	for _, job := range plan.RuntimeBuildJobs {
		jobs[strings.TrimSpace(job.ServiceKey)] = job
	}
	contexts := make(map[string]entity.SelfDeployRuntimeBuildContext, len(plan.RuntimeBuildContexts))
	for _, context := range plan.RuntimeBuildContexts {
		contexts[strings.TrimSpace(context.ServiceKey)] = context
	}
	outputs := make([]SelfDeployBuildOutput, 0, len(buildPlan.BuildItems))
	for _, item := range buildPlan.BuildItems {
		serviceKey := strings.TrimSpace(item.ServiceKey)
		job, ok := jobs[serviceKey]
		if !ok || strings.TrimSpace(job.RuntimeJobRef) == "" || strings.TrimSpace(job.BuildPlanItemFingerprint) != strings.TrimSpace(item.PlanItemFingerprint) {
			return nil, errs.ErrInvalidArgument
		}
		context, ok := contexts[serviceKey]
		if !ok || strings.TrimSpace(context.BuildContextRef) == "" || strings.TrimSpace(context.BuildContextDigest) == "" {
			return nil, errs.ErrInvalidArgument
		}
		spec := item.BuildExecutionSpec
		outputs = append(outputs, SelfDeployBuildOutput{
			ServiceKey:               serviceKey,
			RuntimeJobRef:            job.RuntimeJobRef,
			ImageRef:                 spec.ImageRef,
			ImageTag:                 spec.ImageTag,
			ImageDigest:              spec.ImageDigest,
			BuildPlanItemFingerprint: item.PlanItemFingerprint,
			BuildPlanFingerprint:     buildPlan.PlanFingerprint,
			BuildContextRef:          context.BuildContextRef,
			BuildContextDigest:       context.BuildContextDigest,
		})
	}
	return outputs, nil
}

func validateSelfDeployBuildPlan(plan entity.SelfDeployPlan, buildPlan SelfDeployBuildPlan) error {
	return validateSelfDeployRuntimePlan(buildPlan.PlanFingerprint, len(buildPlan.BuildItems), selfDeployBuildPlanMatchesApprovedPlan(plan, buildPlan), buildPlan.BuildItems, validateSelfDeployBuildPlanItem)
}

func validateSelfDeployDeployPlan(plan entity.SelfDeployPlan, deployPlan SelfDeployDeployPlan) error {
	return validateSelfDeployRuntimePlan(deployPlan.PlanFingerprint, len(deployPlan.DeployItems), selfDeployDeployPlanMatchesApprovedPlan(plan, deployPlan), deployPlan.DeployItems, validateSelfDeployDeployPlanItem)
}

func validateSelfDeployRuntimePlan[T any](fingerprint string, itemCount int, matchesApprovedPlan bool, items []T, validate func(T, string) error) error {
	planFingerprint, err := validateSelfDeployRuntimePlanHeader(fingerprint, itemCount, matchesApprovedPlan)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := validate(item, planFingerprint); err != nil {
			return err
		}
	}
	return nil
}

func validateSelfDeployBuildPlanItem(item SelfDeployBuildPlanItem, fingerprint string) error {
	if err := validateSelfDeployRuntimePlanItem(item.ServiceKey, item.BuildExecutionSpec.ServiceKey, item.BuildExecutionSpec.BuildPlanFingerprint, item.PlanItemFingerprint, fingerprint); err != nil {
		return err
	}
	if err := validateSelfDeployBuildExecutionSpec(item.BuildExecutionSpec, fingerprint); err != nil {
		return errs.ErrDependencyUnavailable
	}
	return nil
}

func validateSelfDeployDeployPlanItem(item SelfDeployDeployPlanItem, fingerprint string) error {
	refsErr := validateSelfDeployRuntimePlanItem(item.ServiceKey, item.DeployExecutionSpec.ServiceKey, item.DeployExecutionSpec.DeployPlanFingerprint, item.PlanItemFingerprint, fingerprint)
	if refsErr != nil {
		return refsErr
	}
	specErr := validateSelfDeployDeployExecutionSpec(item.DeployExecutionSpec, fingerprint)
	if specErr != nil {
		return errs.ErrDependencyUnavailable
	}
	return nil
}

func validateSelfDeployRuntimePlanHeader(fingerprint string, itemCount int, matchesApprovedPlan bool) (string, error) {
	normalized, err := normalizeSHA256Digest(fingerprint)
	if err != nil || normalized == "" || itemCount == 0 {
		return "", errs.ErrDependencyUnavailable
	}
	if !matchesApprovedPlan {
		return "", errs.ErrConflict
	}
	return normalized, nil
}

func validateSelfDeployRuntimePlanItem(serviceKey string, specServiceKey string, specPlanFingerprint string, itemFingerprint string, planFingerprint string) error {
	normalizedItem, err := normalizeSHA256Digest(itemFingerprint)
	if err != nil || normalizedItem == "" {
		return errs.ErrDependencyUnavailable
	}
	if strings.TrimSpace(serviceKey) == "" ||
		strings.TrimSpace(serviceKey) != strings.TrimSpace(specServiceKey) ||
		strings.TrimSpace(specPlanFingerprint) != planFingerprint {
		return errs.ErrDependencyUnavailable
	}
	return nil
}

func validateSelfDeployBuildExecutionSpec(spec SelfDeployBuildExecutionSpec, planFingerprint string) error {
	for _, ref := range []string{spec.SourceRef, spec.ImageRef, spec.BuildContextRef, spec.DockerfileRef, spec.BuilderImageRef} {
		if _, err := normalizeSelfDeployRef(ref, true); err != nil {
			return err
		}
	}
	if _, err := normalizeSelfDeployCommit(spec.SourceCommitSHA); err != nil {
		return err
	}
	if err := validateSelfDeployServiceKey(spec.ServiceKey); err != nil {
		return err
	}
	if err := validateSelfDeployServiceKey(spec.ImageTag); err != nil {
		return err
	}
	if err := validateSelfDeployServiceKey(spec.DockerfileTarget); err != nil {
		return err
	}
	if digest, err := normalizeSHA256Digest(spec.BuildContextDigest); err != nil || digest == "" {
		return errs.ErrInvalidArgument
	}
	if digest, err := normalizeSHA256Digest(spec.BuildPlanFingerprint); err != nil || digest != planFingerprint {
		return errs.ErrInvalidArgument
	}
	if spec.ImageDigest != "" {
		if _, err := normalizeSHA256Digest(spec.ImageDigest); err != nil {
			return err
		}
	}
	if spec.DockerfileDigest != "" {
		if _, err := normalizeSHA256Digest(spec.DockerfileDigest); err != nil {
			return err
		}
	}
	for _, ref := range spec.AllowedSecretRefs {
		if _, err := normalizeSelfDeployRef(ref.SecretRef, true); err != nil {
			return err
		}
		if err := validateSelfDeployServiceKey(ref.Purpose); err != nil {
			return err
		}
	}
	for _, ref := range spec.OutputRefs {
		if err := validateSelfDeployServiceKey(ref.Kind); err != nil {
			return err
		}
		if _, err := normalizeSelfDeployRef(ref.Ref, true); err != nil {
			return err
		}
	}
	return nil
}

func validateSelfDeployDeployExecutionSpec(spec SelfDeployDeployExecutionSpec, planFingerprint string) error {
	for _, ref := range []string{spec.SourceRef, spec.ImageRef, spec.ManifestRef, spec.KustomizationRef, spec.TargetClusterRef, spec.ManifestBundleRef} {
		if _, err := normalizeSelfDeployRef(ref, true); err != nil {
			return err
		}
	}
	if _, err := normalizeSelfDeployCommit(spec.SourceCommitSHA); err != nil {
		return err
	}
	if err := validateSelfDeployServiceKey(spec.ServiceKey); err != nil {
		return err
	}
	if err := validateSelfDeployServiceKey(spec.ImageTag); err != nil {
		return err
	}
	if err := validateSelfDeployServiceKey(spec.TargetNamespace); err != nil {
		return err
	}
	if digest, err := normalizeSHA256Digest(spec.ManifestDigest); err != nil || digest == "" {
		return errs.ErrInvalidArgument
	}
	if digest, err := normalizeSHA256Digest(spec.KustomizationDigest); err != nil || digest == "" {
		return errs.ErrInvalidArgument
	}
	if digest, err := normalizeSHA256Digest(spec.ManifestBundleDigest); err != nil || digest == "" {
		return errs.ErrInvalidArgument
	}
	if digest, err := normalizeSHA256Digest(spec.DeployPlanFingerprint); err != nil || digest != planFingerprint {
		return errs.ErrInvalidArgument
	}
	if spec.ImageDigest != "" {
		if _, err := normalizeSHA256Digest(spec.ImageDigest); err != nil {
			return err
		}
	}
	for _, ref := range spec.AllowedSecretRefs {
		if _, err := normalizeSelfDeployRef(ref.SecretRef, true); err != nil {
			return err
		}
		if err := validateSelfDeployServiceKey(ref.Purpose); err != nil {
			return err
		}
	}
	for _, ref := range spec.OutputRefs {
		if err := validateSelfDeployServiceKey(ref.Kind); err != nil {
			return err
		}
		if _, err := normalizeSelfDeployRef(ref.Ref, true); err != nil {
			return err
		}
	}
	if len(spec.RolloutTargets) == 0 || len(spec.ExpectedImageRefs) == 0 {
		return errs.ErrInvalidArgument
	}
	for _, target := range spec.RolloutTargets {
		if err := validateSelfDeployServiceKey(target.Kind); err != nil {
			return err
		}
		if _, err := normalizeSelfDeployRef(target.Ref, true); err != nil {
			return err
		}
		if err := validateSelfDeployServiceKey(target.Namespace); err != nil {
			return err
		}
		if err := validateSelfDeployServiceKey(target.Name); err != nil {
			return err
		}
		if target.Digest != "" {
			if _, err := normalizeSHA256Digest(target.Digest); err != nil {
				return err
			}
		}
	}
	for _, ref := range spec.ExpectedImageRefs {
		if err := validateSelfDeployServiceKey(ref.ContainerName); err != nil {
			return err
		}
		if _, err := normalizeSelfDeployRef(ref.ImageRef, true); err != nil {
			return err
		}
		if ref.ImageDigest != "" {
			if _, err := normalizeSHA256Digest(ref.ImageDigest); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) createSelfDeployBuildJobs(ctx context.Context, plan entity.SelfDeployPlan, buildPlan SelfDeployBuildPlan) ([]entity.SelfDeployRuntimeBuildJob, error) {
	lookup, err := selfDeployBuildPlanLookupInput(plan)
	if err != nil {
		return nil, err
	}
	return createSelfDeployRuntimeJobs(buildPlan.BuildItems, func(item SelfDeployBuildPlanItem) (entity.SelfDeployRuntimeBuildJob, error) {
		result, err := s.selfDeployBuildJobCreator.CreateSelfDeployBuildJob(ctx, SelfDeployBuildJobInput{
			SelfDeployRuntimeJobInput: selfDeployRuntimeBuildJobInput(plan, lookup, item.ServiceKey, item.ServiceRef, buildPlan.PlanFingerprint, item.PlanItemFingerprint, selfDeployRuntimeBuildCommandMeta(plan.ID, item)),
			BuildExecutionSpec:        item.BuildExecutionSpec,
		})
		if err != nil {
			return entity.SelfDeployRuntimeBuildJob{}, err
		}
		refs, err := selfDeployRuntimeJobSafeRefs(result.JobRef, item.ServiceRef)
		if err != nil {
			return entity.SelfDeployRuntimeBuildJob{}, selfDeployRuntimeRefError()
		}
		return entity.SelfDeployRuntimeBuildJob{
			ServiceKey:               item.ServiceKey,
			ServiceRef:               refs.serviceRef,
			RuntimeJobRef:            refs.jobRef,
			RuntimeJobStatus:         result.Status,
			BuildPlanItemFingerprint: item.PlanItemFingerprint,
		}, nil
	})
}

func (s *Service) createSelfDeployDeployJobs(ctx context.Context, plan entity.SelfDeployPlan, deployPlan SelfDeployDeployPlan) ([]entity.SelfDeployRuntimeDeployJob, error) {
	lookup, err := selfDeployBuildPlanLookupInput(plan)
	if err != nil {
		return nil, err
	}
	return createSelfDeployRuntimeJobs(deployPlan.DeployItems, func(item SelfDeployDeployPlanItem) (entity.SelfDeployRuntimeDeployJob, error) {
		input := SelfDeployDeployJobInput{DeployExecutionSpec: item.DeployExecutionSpec}
		input.SelfDeployRuntimeJobInput = selfDeployRuntimeBuildJobInput(plan, lookup, item.ServiceKey, item.ServiceRef, deployPlan.PlanFingerprint, item.PlanItemFingerprint, selfDeployRuntimeDeployCommandMeta(plan.ID, item))
		result, err := s.selfDeployDeployJobCreator.CreateSelfDeployDeployJob(ctx, input)
		if err != nil {
			return entity.SelfDeployRuntimeDeployJob{}, err
		}
		refs, err := selfDeployRuntimeJobSafeRefs(result.JobRef, item.ServiceRef)
		if err != nil {
			return entity.SelfDeployRuntimeDeployJob{}, selfDeployRuntimeRefError()
		}
		job := entity.SelfDeployRuntimeDeployJob{}
		job.ServiceKey = item.ServiceKey
		job.ServiceRef = refs.serviceRef
		job.RuntimeJobRef = refs.jobRef
		job.RuntimeJobStatus = result.Status
		job.DeployPlanItemFingerprint = item.PlanItemFingerprint
		return job, nil
	})
}

func selfDeployRuntimeBuildJobInput(plan entity.SelfDeployPlan, lookup SelfDeployBuildPlanLookupInput, serviceKey string, serviceRef string, planFingerprint string, planItemFingerprint string, meta value.CommandMeta) SelfDeployRuntimeJobInput {
	return SelfDeployRuntimeJobInput{
		Meta:                  meta,
		ProjectID:             lookup.ProjectID,
		RepositoryID:          lookup.RepositoryID,
		PlanID:                plan.ID,
		ServiceKey:            serviceKey,
		ServiceRef:            serviceRef,
		PlanFingerprint:       planFingerprint,
		PlanItemFingerprint:   planItemFingerprint,
		GovernanceApprovalRef: plan.GovernanceContext.GateDecisionRef,
		GovernanceGateRef:     plan.GovernanceContext.GateRequestRef,
	}
}

type selfDeployRuntimeJobRefs struct {
	jobRef     string
	serviceRef string
}

func selfDeployRuntimeJobSafeRefs(jobRef string, serviceRef string) (selfDeployRuntimeJobRefs, error) {
	normalizedJobRef, err := normalizeSelfDeployRef(jobRef, true)
	if err != nil {
		return selfDeployRuntimeJobRefs{}, err
	}
	normalizedServiceRef, err := normalizeSelfDeployRef(serviceRef, false)
	if err != nil {
		return selfDeployRuntimeJobRefs{}, err
	}
	return selfDeployRuntimeJobRefs{jobRef: normalizedJobRef, serviceRef: normalizedServiceRef}, nil
}

func createSelfDeployRuntimeJobs[T any, J any](items []T, create func(T) (J, error)) ([]J, error) {
	jobs := make([]J, len(items))
	for index := 0; index < len(items); index++ {
		job, err := create(items[index])
		if err != nil {
			return nil, err
		}
		jobs[index] = job
	}
	return jobs, nil
}

func selfDeployRuntimeRefError() error {
	return NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager returned unsafe build job refs")
}

func (s *Service) recordSelfDeployBuildRequested(ctx context.Context, plan entity.SelfDeployPlan, buildPlan SelfDeployBuildPlan, jobs []entity.SelfDeployRuntimeBuildJob) (entity.SelfDeployPlan, error) {
	plan.RuntimeBuildJobs = append([]entity.SelfDeployRuntimeBuildJob(nil), jobs...)
	plan = withSelfDeployBuildRuntimeProgress(plan, enum.SelfDeployRuntimeBuildStatusRequested, buildPlan.PlanFingerprint, "", "self-deploy build jobs requested")
	return s.recordSelfDeployBuildState(ctx, plan)
}

func (s *Service) recordSelfDeployBuildSucceeded(ctx context.Context, plan entity.SelfDeployPlan) (entity.SelfDeployPlan, error) {
	plan = withSelfDeployBuildRuntimeProgress(plan, enum.SelfDeployRuntimeBuildStatusSucceeded, plan.RuntimeBuildFingerprint, "", "self-deploy build jobs succeeded")
	return s.recordSelfDeployBuildState(ctx, plan)
}

func (s *Service) recordSelfDeployDeployRequested(ctx context.Context, plan entity.SelfDeployPlan, deployPlan SelfDeployDeployPlan, jobs []entity.SelfDeployRuntimeDeployJob) (entity.SelfDeployPlan, error) {
	copiedJobs := append([]entity.SelfDeployRuntimeDeployJob(nil), jobs...)
	plan.RuntimeDeployJobs = copiedJobs
	plan = withSelfDeployDeployRuntimeProgress(plan, enum.SelfDeployRuntimeDeployStatusRequested, deployPlan.PlanFingerprint, "", "self-deploy deploy jobs requested")
	return s.recordSelfDeployDeployState(ctx, plan)
}

func (s *Service) recordSelfDeployDeploySucceeded(ctx context.Context, plan entity.SelfDeployPlan) (entity.SelfDeployPlan, error) {
	plan = withSelfDeployDeployRuntimeProgress(plan, enum.SelfDeployRuntimeDeployStatusSucceeded, plan.RuntimeDeployFingerprint, "", "self-deploy deploy jobs succeeded")
	return s.recordSelfDeployDeployState(ctx, plan)
}

func (s *Service) recordSelfDeployBuildBlocked(ctx context.Context, plan entity.SelfDeployPlan, code string, summary string, fingerprint string) (entity.SelfDeployPlan, error) {
	plan = withSelfDeployPolicyStaleTerminalStatus(plan, code, summary)
	return s.recordSelfDeployBuildDiagnostic(ctx, plan, enum.SelfDeployRuntimeBuildStatusBlocked, code, summary, fingerprint)
}

func (s *Service) recordSelfDeployTerminalBlocker(
	ctx context.Context,
	plan entity.SelfDeployPlan,
	code string,
	summary string,
	fingerprint string,
	policyStaleCode string,
	record func(context.Context, entity.SelfDeployPlan, string, string, string) (entity.SelfDeployPlan, error),
) (entity.SelfDeployPlan, error) {
	if plan.Status == enum.SelfDeployPlanStatusFailed || strings.TrimSpace(code) != policyStaleCode {
		return plan, nil
	}
	return record(ctx, plan, code, summary, fingerprint)
}

func (s *Service) recordSelfDeployDeployBlocked(ctx context.Context, plan entity.SelfDeployPlan, code string, summary string, fingerprint string) (entity.SelfDeployPlan, error) {
	plan = withSelfDeployPolicyStaleTerminalStatus(plan, code, summary)
	blockedStatus := enum.SelfDeployRuntimeDeployStatusBlocked
	return s.recordSelfDeployDeployDiagnostic(ctx, plan, blockedStatus, code, summary, fingerprint)
}

func (s *Service) recordSelfDeployBuildPreparingContext(ctx context.Context, plan entity.SelfDeployPlan, code SelfDeployBuildPlanStatus, summary string, fingerprint string) (entity.SelfDeployPlan, error) {
	return s.recordSelfDeployBuildDiagnostic(ctx, plan, enum.SelfDeployRuntimeBuildStatusPreparingContext, string(code), summary, fingerprint)
}

func (s *Service) recordSelfDeployBuildDiagnostic(ctx context.Context, plan entity.SelfDeployPlan, status enum.SelfDeployRuntimeBuildStatus, code string, summary string, fingerprint string) (entity.SelfDeployPlan, error) {
	return s.recordSelfDeployBuildState(ctx, withSelfDeployBuildRuntimeProgress(plan, status, fingerprint, code, summary))
}

func (s *Service) recordSelfDeployDeployDiagnostic(ctx context.Context, plan entity.SelfDeployPlan, status enum.SelfDeployRuntimeDeployStatus, code string, summary string, fingerprint string) (entity.SelfDeployPlan, error) {
	updated := withSelfDeployDeployRuntimeProgress(plan, status, fingerprint, code, summary)
	return s.recordSelfDeployDeployState(ctx, updated)
}

func (s *Service) recordSelfDeployBuildFailure(ctx context.Context, plan entity.SelfDeployPlan, failure runtimeOperationFailure, fingerprint string) (entity.SelfDeployPlan, error) {
	return s.recordSelfDeployBuildDiagnostic(ctx, plan, enum.SelfDeployRuntimeBuildStatusFailed, failure.code, failure.summary(), fingerprint)
}

func (s *Service) recordSelfDeployDeployFailure(ctx context.Context, plan entity.SelfDeployPlan, failure runtimeOperationFailure, fingerprint string) (entity.SelfDeployPlan, error) {
	code := failure.code
	summary := failure.summary()
	return s.recordSelfDeployDeployDiagnostic(ctx, plan, enum.SelfDeployRuntimeDeployStatusFailed, code, summary, fingerprint)
}

func withSelfDeployBuildRuntimeProgress(plan entity.SelfDeployPlan, status enum.SelfDeployRuntimeBuildStatus, fingerprint string, code string, summary string) entity.SelfDeployPlan {
	plan.RuntimeBuildStatus = status
	setSelfDeployRuntimeProgress(&plan.RuntimeBuildFingerprint, &plan.RuntimeBuildErrorCode, &plan.RuntimeBuildSummary, fingerprint, code, summary)
	return plan
}

func withSelfDeployDeployRuntimeProgress(plan entity.SelfDeployPlan, status enum.SelfDeployRuntimeDeployStatus, fingerprint string, code string, summary string) entity.SelfDeployPlan {
	setSelfDeployRuntimeProgress(&plan.RuntimeDeployFingerprint, &plan.RuntimeDeployErrorCode, &plan.RuntimeDeploySummary, fingerprint, code, summary)
	plan.RuntimeDeployStatus = status
	return plan
}

func setSelfDeployRuntimeProgress(fingerprintField *string, codeField *string, summaryField *string, fingerprint string, code string, summary string) {
	*fingerprintField = strings.TrimSpace(fingerprint)
	*codeField = selfDeploySafeSummary(code)
	*summaryField = selfDeploySafeSummary(summary)
}

func withSelfDeployPolicyStaleTerminalStatus(plan entity.SelfDeployPlan, code string, summary string) entity.SelfDeployPlan {
	if strings.TrimSpace(code) != string(SelfDeployBuildPlanStatusPolicyStale) &&
		strings.TrimSpace(code) != string(SelfDeployDeployPlanStatusPolicyStale) {
		return plan
	}
	plan.Status = enum.SelfDeployPlanStatusFailed
	plan.SafeSummary = selfDeploySafeSummary(firstNonEmpty(summary, "self-deploy services policy is stale; create a new checked plan from a fresh provider/project signal"))
	return plan
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

func (s *Service) recordSelfDeployDeployState(ctx context.Context, plan entity.SelfDeployPlan) (entity.SelfDeployPlan, error) {
	loaded, err := s.repository.GetSelfDeployPlan(ctx, plan.ID)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	if sameSelfDeployRuntimeDeployState(loaded, plan) {
		return loaded, nil
	}
	if loaded.Version != plan.Version {
		if selfDeployDeployJobsRequested(loaded) || selfDeployDeployJobsSucceeded(loaded) {
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
	command, err := commandResult(selfDeployDeployStateCommandMeta(plan), operationDispatchSelfDeployDeploy, enum.CommandAggregateTypeSelfDeployPlan, plan.ID, payload, now)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	event, err := selfDeployPlanRequestedEvent(s.idGenerator.New(), plan, now)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	if err := s.repository.UpdateSelfDeployPlanWithResult(ctx, plan, previousVersion, command, &event); err != nil {
		return s.resolveSelfDeployDeployUpdateError(ctx, plan, err)
	}
	return plan, nil
}

func selfDeployBuildPlanMatchesApprovedPlan(plan entity.SelfDeployPlan, buildPlan SelfDeployBuildPlan) bool {
	return selfDeployRuntimePlanMatchesApprovedPlan(
		plan,
		buildPlan.ProjectRef,
		buildPlan.RepositoryRef,
		buildPlan.SourceRef,
		buildPlan.MergeCommitSHA,
		buildPlan.ServicesYAML.Digest,
		buildPlan.AffectedServiceKeys,
	)
}

func selfDeployDeployPlanMatchesApprovedPlan(plan entity.SelfDeployPlan, deployPlan SelfDeployDeployPlan) bool {
	return selfDeployRuntimePlanMatchesApprovedPlan(
		plan,
		deployPlan.ProjectRef,
		deployPlan.RepositoryRef,
		deployPlan.SourceRef,
		deployPlan.MergeCommitSHA,
		deployPlan.ServicesYAML.Digest,
		deployPlan.AffectedServiceKeys,
	)
}

func selfDeployRuntimePlanMatchesApprovedPlan(plan entity.SelfDeployPlan, projectRef string, repositoryRef string, sourceRef string, commitSHA string, servicesDigest string, serviceKeys []string) bool {
	if strings.TrimSpace(projectRef) != strings.TrimSpace(plan.ProjectRef) {
		return false
	}
	if strings.TrimSpace(repositoryRef) != strings.TrimSpace(plan.RepositoryRef) {
		return false
	}
	if strings.TrimSpace(sourceRef) != strings.TrimSpace(plan.SourceRef) {
		return false
	}
	if strings.TrimSpace(commitSHA) != strings.TrimSpace(plan.MergeCommitSHA) {
		return false
	}
	if strings.TrimSpace(servicesDigest) != strings.TrimSpace(plan.ServicesYAMLDigest) {
		return false
	}
	return stringSlicesSetEqual(serviceKeys, plan.AffectedServiceKeys)
}

func (s *Service) resolveSelfDeployBuildUpdateError(ctx context.Context, desired entity.SelfDeployPlan, updateErr error) (entity.SelfDeployPlan, error) {
	loaded, loadErr := s.repository.GetSelfDeployPlan(ctx, desired.ID)
	if loadErr == nil && (selfDeployBuildJobsRequested(loaded) || sameSelfDeployRuntimeBuildState(loaded, desired)) {
		return loaded, nil
	}
	return entity.SelfDeployPlan{}, updateErr
}

func (s *Service) resolveSelfDeployDeployUpdateError(ctx context.Context, desired entity.SelfDeployPlan, updateErr error) (entity.SelfDeployPlan, error) {
	loaded, loadErr := s.repository.GetSelfDeployPlan(ctx, desired.ID)
	if loadErr == nil && (selfDeployDeployJobsRequested(loaded) || selfDeployDeployJobsSucceeded(loaded) || sameSelfDeployRuntimeDeployState(loaded, desired)) {
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

func selfDeployDeployPlanBlockedSummary(read SelfDeployDeployPlanReadResult) string {
	if strings.TrimSpace(read.SafeReason) != "" {
		return "self-deploy deploy plan is not ready: " + strings.TrimSpace(read.SafeReason)
	}
	return "self-deploy deploy plan is not ready"
}

func selfDeployBuildCommandMeta(planID uuid.UUID) value.CommandMeta {
	return value.CommandMeta{
		IdempotencyKey: "self_deploy_build:" + planID.String(),
		Actor:          value.Actor{Type: "service", ID: "agent-manager"},
	}
}

func selfDeployBuildContextCommandMeta(planID uuid.UUID, fingerprint string) value.CommandMeta {
	return value.CommandMeta{
		CommandID:      uuid.NewSHA1(selfDeployContextRuntimeCommandNamespace, []byte("runtime-build-context:"+planID.String()+":"+strings.TrimSpace(fingerprint))),
		IdempotencyKey: "self_deploy_build_context:" + planID.String(),
		Actor:          value.Actor{Type: "service", ID: "agent-manager"},
	}
}

func selfDeployDeployCommandMeta(planID uuid.UUID) value.CommandMeta {
	return value.CommandMeta{
		IdempotencyKey: "self_deploy_deploy:" + planID.String(),
		Actor:          value.Actor{Type: "service", ID: "agent-manager"},
	}
}

func selfDeployRuntimeJobQueryMeta() value.QueryMeta {
	return value.QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}}
}

func selfDeployBuildStateCommandMeta(plan entity.SelfDeployPlan) value.CommandMeta {
	return selfDeployRuntimeStateCommandMeta("self_deploy_build", plan.ID, string(plan.RuntimeBuildStatus), plan.RuntimeBuildFingerprint, plan.RuntimeBuildErrorCode, plan.RuntimeBuildSummary)
}

func selfDeployDeployStateCommandMeta(plan entity.SelfDeployPlan) value.CommandMeta {
	return selfDeployRuntimeStateCommandMeta("self_deploy_deploy", plan.ID, string(plan.RuntimeDeployStatus), plan.RuntimeDeployFingerprint, plan.RuntimeDeployErrorCode, plan.RuntimeDeploySummary)
}

func selfDeployRuntimeStateCommandMeta(prefix string, planID uuid.UUID, status string, fingerprint string, code string, summary string) value.CommandMeta {
	return value.CommandMeta{
		IdempotencyKey: strings.Join([]string{
			prefix,
			planID.String(),
			status,
			selfDeployBuildKeyDigest(fingerprint),
			selfDeployBuildKeyDigest(code),
			selfDeployBuildKeyDigest(summary),
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
	return selfDeployRuntimeJobCommandMeta(selfDeployBuildRuntimeCommandNamespace, "runtime-build", "self_deploy_build_job", planID, item.ServiceKey, item.PlanItemFingerprint)
}

func selfDeployRuntimeDeployCommandMeta(planID uuid.UUID, item SelfDeployDeployPlanItem) value.CommandMeta {
	return selfDeployRuntimeJobCommandMeta(selfDeployDeployRuntimeCommandNamespace, "runtime-deploy", "self_deploy_deploy_job", planID, item.ServiceKey, item.PlanItemFingerprint)
}

func selfDeployRuntimeJobCommandMeta(namespace uuid.UUID, commandPrefix string, idempotencyPrefix string, planID uuid.UUID, serviceKey string, fingerprint string) value.CommandMeta {
	normalizedServiceKey := strings.TrimSpace(serviceKey)
	normalizedFingerprint := strings.TrimSpace(fingerprint)
	return value.CommandMeta{
		CommandID:      uuid.NewSHA1(namespace, []byte(commandPrefix+":"+planID.String()+":"+normalizedServiceKey+":"+normalizedFingerprint)),
		IdempotencyKey: idempotencyPrefix + ":" + planID.String() + ":" + normalizedServiceKey,
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sameSelfDeployRuntimeBuildState(left entity.SelfDeployPlan, right entity.SelfDeployPlan) bool {
	return sameSelfDeployPlanLifecycleState(left, right) &&
		sameSelfDeployRuntimeProgress(string(left.RuntimeBuildStatus), string(right.RuntimeBuildStatus), left.RuntimeBuildFingerprint, right.RuntimeBuildFingerprint, left.RuntimeBuildErrorCode, right.RuntimeBuildErrorCode, left.RuntimeBuildSummary, right.RuntimeBuildSummary) &&
		sameSelfDeployRuntimeBuildContexts(left.RuntimeBuildContexts, right.RuntimeBuildContexts) &&
		sameSelfDeployRuntimeBuildJobs(left.RuntimeBuildJobs, right.RuntimeBuildJobs)
}

func sameSelfDeployRuntimeDeployState(left entity.SelfDeployPlan, right entity.SelfDeployPlan) bool {
	return sameSelfDeployPlanLifecycleState(left, right) &&
		sameSelfDeployRuntimeProgress(string(left.RuntimeDeployStatus), string(right.RuntimeDeployStatus), left.RuntimeDeployFingerprint, right.RuntimeDeployFingerprint, left.RuntimeDeployErrorCode, right.RuntimeDeployErrorCode, left.RuntimeDeploySummary, right.RuntimeDeploySummary) &&
		sameSelfDeployRuntimeDeployJobs(left.RuntimeDeployJobs, right.RuntimeDeployJobs)
}

func sameSelfDeployPlanLifecycleState(left entity.SelfDeployPlan, right entity.SelfDeployPlan) bool {
	return left.Status == right.Status &&
		strings.TrimSpace(left.SafeSummary) == strings.TrimSpace(right.SafeSummary)
}

func sameSelfDeployRuntimeProgress(leftStatus string, rightStatus string, leftFingerprint string, rightFingerprint string, leftCode string, rightCode string, leftSummary string, rightSummary string) bool {
	return leftStatus == rightStatus &&
		strings.TrimSpace(leftFingerprint) == strings.TrimSpace(rightFingerprint) &&
		strings.TrimSpace(leftCode) == strings.TrimSpace(rightCode) &&
		strings.TrimSpace(leftSummary) == strings.TrimSpace(rightSummary)
}

func sameSelfDeployRuntimeBuildContexts(left []entity.SelfDeployRuntimeBuildContext, right []entity.SelfDeployRuntimeBuildContext) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if strings.TrimSpace(left[index].ServiceKey) != strings.TrimSpace(right[index].ServiceKey) ||
			strings.TrimSpace(left[index].RuntimeBuildContextRef) != strings.TrimSpace(right[index].RuntimeBuildContextRef) ||
			strings.TrimSpace(left[index].RuntimeBuildContextStatus) != strings.TrimSpace(right[index].RuntimeBuildContextStatus) ||
			strings.TrimSpace(left[index].BuildContextRef) != strings.TrimSpace(right[index].BuildContextRef) ||
			strings.TrimSpace(left[index].BuildContextDigest) != strings.TrimSpace(right[index].BuildContextDigest) ||
			strings.TrimSpace(left[index].ManifestBundleDigest) != strings.TrimSpace(right[index].ManifestBundleDigest) ||
			strings.TrimSpace(left[index].MaterializationFingerprint) != strings.TrimSpace(right[index].MaterializationFingerprint) ||
			strings.TrimSpace(left[index].BuildPlanItemFingerprint) != strings.TrimSpace(right[index].BuildPlanItemFingerprint) {
			return false
		}
	}
	return true
}

func sameSelfDeployRuntimeBuildJobs(left []entity.SelfDeployRuntimeBuildJob, right []entity.SelfDeployRuntimeBuildJob) bool {
	return sameSelfDeployRuntimeJobList(len(left), len(right), func(index int) bool {
		return sameSelfDeployRuntimeJobRef(left[index].ServiceKey, right[index].ServiceKey, left[index].ServiceRef, right[index].ServiceRef, left[index].RuntimeJobRef, right[index].RuntimeJobRef, left[index].RuntimeJobStatus, right[index].RuntimeJobStatus, left[index].BuildPlanItemFingerprint, right[index].BuildPlanItemFingerprint)
	})
}

func sameSelfDeployRuntimeDeployJobs(left []entity.SelfDeployRuntimeDeployJob, right []entity.SelfDeployRuntimeDeployJob) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		current := left[index]
		desired := right[index]
		if !sameSelfDeployRuntimeJobRef(current.ServiceKey, desired.ServiceKey, current.ServiceRef, desired.ServiceRef, current.RuntimeJobRef, desired.RuntimeJobRef, current.RuntimeJobStatus, desired.RuntimeJobStatus, current.DeployPlanItemFingerprint, desired.DeployPlanItemFingerprint) {
			return false
		}
	}
	return true
}

func sameSelfDeployRuntimeJobList(leftLen int, rightLen int, compare func(int) bool) bool {
	if leftLen != rightLen {
		return false
	}
	for index := 0; index < leftLen; index++ {
		if !compare(index) {
			return false
		}
	}
	return true
}

func sameSelfDeployRuntimeJobRef(leftServiceKey string, rightServiceKey string, leftServiceRef string, rightServiceRef string, leftJobRef string, rightJobRef string, leftStatus string, rightStatus string, leftFingerprint string, rightFingerprint string) bool {
	return strings.TrimSpace(leftServiceKey) == strings.TrimSpace(rightServiceKey) &&
		strings.TrimSpace(leftServiceRef) == strings.TrimSpace(rightServiceRef) &&
		strings.TrimSpace(leftJobRef) == strings.TrimSpace(rightJobRef) &&
		strings.TrimSpace(leftStatus) == strings.TrimSpace(rightStatus) &&
		strings.TrimSpace(leftFingerprint) == strings.TrimSpace(rightFingerprint)
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
