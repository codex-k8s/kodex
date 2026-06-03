package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

const (
	selfDeployPlanTargetType          = "self_deploy_plan"
	selfDeployPlanEvidenceKind        = "self_deploy_plan"
	selfDeployPlanGateEvidenceSummary = "self-deploy plan gate input"
)

type selfDeployPlanGateCommandPayload struct {
	SelfDeployPlanRef string `json:"self_deploy_plan_ref"`
	PlanFingerprint   string `json:"plan_fingerprint"`
	GateRequestID     string `json:"gate_request_id,omitempty"`
	Status            string `json:"status"`
}

type normalizedSelfDeployPlanGateInput struct {
	SelfDeployPlanRef       string
	ProjectContext          value.ProjectContextRef
	ProviderSignalRef       string
	SourceRef               string
	MergeCommitSHA          string
	ServicesYAMLRef         string
	ServicesYAMLDigest      string
	AffectedServiceKeys     []string
	PathCategories          []string
	ExpectedRuntimeJobTypes []string
	ChangedFilesSummaryRef  string
	SafeSummary             string
	PlanFingerprint         string
	EvidenceRefs            []value.EvidenceRef
	RiskProfileRef          string
}

func (s *Service) prepareSelfDeployPlanGate(ctx context.Context, input SelfDeployPlanGateInput) (SelfDeployPlanGateResult, error) {
	if err := requireCommand(input.Meta, enum.OperationPrepareSelfDeployPlanGate.String()); err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	normalized, err := normalizeSelfDeployPlanGateInput(input)
	if err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	target := selfDeployPlanTarget(normalized.SelfDeployPlanRef)
	queryMeta := queryMetaFromCommandMeta(input.Meta)
	commandResult, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationPrepareSelfDeployPlanGate.String(), aggregateSelfDeployPlanGate)
	if err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	if replayed {
		return s.replayedSelfDeployPlanGateResult(ctx, commandResult, normalized, target, queryMeta)
	}
	assessment, err := s.findOrCreateSelfDeployRiskAssessment(ctx, normalized, target, input.Meta, queryMeta)
	if err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	gateRequest, err := s.findOrCreateSelfDeployGateRequest(ctx, normalized, target, assessment, input.Meta, queryMeta)
	if err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	decision, err := s.selfDeployGateDecision(ctx, gateRequest, queryMeta)
	if err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	status := selfDeployPlanGateStatus(assessment, gateRequest, decision)
	summary, err := s.GetGovernanceSummary(ctx, GetGovernanceSummaryInput{
		Scope: entity.GovernanceSummaryScope{Target: target},
		Meta:  queryMeta,
	})
	if err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	result := SelfDeployPlanGateResult{
		Status:            status,
		RiskAssessment:    assessment,
		GateRequest:       gateRequest,
		GateDecision:      decision,
		GovernanceSummary: summary,
	}
	if err := s.recordSelfDeployPlanGateCommandResult(ctx, input.Meta, normalized, assessment, gateRequest, status); err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	return result, nil
}

func (s *Service) replayedSelfDeployPlanGateResult(ctx context.Context, result entity.CommandResult, input normalizedSelfDeployPlanGateInput, target value.ExternalRef, meta QueryMeta) (SelfDeployPlanGateResult, error) {
	payload, err := selfDeployPlanGatePayload(result)
	if err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	if payload.SelfDeployPlanRef != input.SelfDeployPlanRef || payload.PlanFingerprint != input.PlanFingerprint {
		return SelfDeployPlanGateResult{}, errs.ErrConflict
	}
	assessment, err := s.GetRiskAssessment(ctx, GetRiskAssessmentInput{RiskAssessmentID: result.AggregateID, Meta: meta})
	if err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	if !sameExternalRef(assessment.Target, target) || !selfDeployAssessmentMatchesFingerprint(assessment, input) {
		return SelfDeployPlanGateResult{}, errs.ErrConflict
	}
	var gateRequest entity.GateRequest
	if payload.GateRequestID != "" {
		gateRequestID, err := uuid.Parse(payload.GateRequestID)
		if err != nil {
			return SelfDeployPlanGateResult{}, errs.ErrConflict
		}
		gateRequest, err = s.GetGateRequest(ctx, GetGateRequestInput{GateRequestID: gateRequestID, Meta: meta})
		if err != nil {
			return SelfDeployPlanGateResult{}, err
		}
	}
	decision, err := s.selfDeployGateDecision(ctx, gateRequest, meta)
	if err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	summary, err := s.GetGovernanceSummary(ctx, GetGovernanceSummaryInput{Scope: entity.GovernanceSummaryScope{Target: target}, Meta: meta})
	if err != nil {
		return SelfDeployPlanGateResult{}, err
	}
	return SelfDeployPlanGateResult{
		Status:            selfDeployPlanGateStatus(assessment, gateRequest, decision),
		RiskAssessment:    assessment,
		GateRequest:       gateRequest,
		GateDecision:      decision,
		GovernanceSummary: summary,
	}, nil
}

func (s *Service) recordSelfDeployPlanGateCommandResult(ctx context.Context, meta CommandMeta, input normalizedSelfDeployPlanGateInput, assessment entity.RiskAssessment, gateRequest entity.GateRequest, status enum.SelfDeployPlanGateStatus) error {
	payload := map[string]any{
		"self_deploy_plan_ref": input.SelfDeployPlanRef,
		"plan_fingerprint":     input.PlanFingerprint,
		"risk_assessment_id":   assessment.ID.String(),
		"status":               string(status),
	}
	if gateRequest.ID != uuid.Nil {
		payload["gate_request_id"] = gateRequest.ID.String()
	}
	result := commandResultWithPayload(meta, enum.OperationPrepareSelfDeployPlanGate.String(), aggregateSelfDeployPlanGate, assessment.ID, s.clock.Now(), payload)
	return s.repository.RecordCommandResult(ctx, result)
}

func selfDeployPlanGatePayload(result entity.CommandResult) (selfDeployPlanGateCommandPayload, error) {
	var payload selfDeployPlanGateCommandPayload
	if err := json.Unmarshal(result.ResultPayload, &payload); err != nil {
		return selfDeployPlanGateCommandPayload{}, errs.ErrConflict
	}
	if payload.SelfDeployPlanRef == "" || payload.PlanFingerprint == "" {
		return selfDeployPlanGateCommandPayload{}, errs.ErrConflict
	}
	return payload, nil
}

func (s *Service) findOrCreateSelfDeployRiskAssessment(ctx context.Context, input normalizedSelfDeployPlanGateInput, target value.ExternalRef, meta CommandMeta, queryMeta QueryMeta) (entity.RiskAssessment, error) {
	assessments, _, err := s.ListRiskAssessments(ctx, ListRiskAssessmentsInput{
		Filter: query.RiskAssessmentFilter{Target: target, Page: query.PageRequest{PageSize: 1}},
		Meta:   queryMeta,
	})
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	if len(assessments) > 0 {
		if !selfDeployAssessmentMatchesFingerprint(assessments[0], input) {
			return entity.RiskAssessment{}, errs.ErrConflict
		}
		return assessments[0], nil
	}
	return s.EvaluateRisk(ctx, EvaluateRiskInput{
		Target:            target,
		ProjectContext:    input.ProjectContext,
		EvidenceRefs:      selfDeployPlanEvidenceRefs(input),
		RiskProfileRef:    input.RiskProfileRef,
		EvaluationSummary: selfDeployPlanRiskSummary(input),
		Meta:              derivedSelfDeployCommandMeta(meta, "risk", input.PlanFingerprint),
	})
}

func (s *Service) findOrCreateSelfDeployGateRequest(ctx context.Context, input normalizedSelfDeployPlanGateInput, target value.ExternalRef, assessment entity.RiskAssessment, meta CommandMeta, queryMeta QueryMeta) (entity.GateRequest, error) {
	assessmentID := assessment.ID
	requests, _, err := s.ListGateRequests(ctx, ListGateRequestsInput{
		Filter: query.GateRequestFilter{RiskAssessmentID: &assessmentID, Page: query.PageRequest{PageSize: 10}},
		Meta:   queryMeta,
	})
	if err != nil {
		return entity.GateRequest{}, err
	}
	if request, ok := selectSelfDeployGateRequest(requests); ok {
		return request, nil
	}
	if !riskClassAtLeast(assessment.EffectiveRiskClass, enum.RiskClassR2) && len(assessment.RequiredGates) == 0 {
		return entity.GateRequest{}, nil
	}
	gatePolicyID := selfDeployGatePolicyRef(assessment.RequiredGates)
	return s.RequestGate(ctx, RequestGateInput{
		RiskAssessmentID: &assessmentID,
		GatePolicyID:     gatePolicyID,
		Target:           target,
		EvidenceRefs:     selfDeployPlanGateEvidenceRefs(input),
		EvidenceSummary:  selfDeployPlanGateSummary(input, assessment),
		Meta:             derivedSelfDeployCommandMeta(meta, "gate", input.PlanFingerprint),
	})
}

func (s *Service) selfDeployGateDecision(ctx context.Context, request entity.GateRequest, meta QueryMeta) (*entity.GateDecision, error) {
	if request.ID == uuid.Nil {
		return nil, nil
	}
	requestID := request.ID
	decisions, _, err := s.ListGateDecisions(ctx, ListGateDecisionsInput{
		Filter: query.GateDecisionFilter{GateRequestID: &requestID, Page: query.PageRequest{PageSize: 1}},
		Meta:   meta,
	})
	if err != nil {
		return nil, err
	}
	if len(decisions) == 0 {
		return nil, nil
	}
	return &decisions[0], nil
}

func normalizeSelfDeployPlanGateInput(input SelfDeployPlanGateInput) (normalizedSelfDeployPlanGateInput, error) {
	projectContext, err := normalizeReleaseProjectContext(input.ProjectContext)
	if err != nil {
		return normalizedSelfDeployPlanGateInput{}, err
	}
	normalized := normalizedSelfDeployPlanGateInput{
		SelfDeployPlanRef:      strings.TrimSpace(input.SelfDeployPlanRef),
		ProjectContext:         projectContext,
		ProviderSignalRef:      strings.TrimSpace(input.ProviderSignalRef),
		SourceRef:              strings.TrimSpace(input.SourceRef),
		MergeCommitSHA:         strings.TrimSpace(input.MergeCommitSHA),
		ServicesYAMLRef:        strings.TrimSpace(input.ServicesYAMLRef),
		ServicesYAMLDigest:     strings.TrimSpace(input.ServicesYAMLDigest),
		ChangedFilesSummaryRef: strings.TrimSpace(input.ChangedFilesSummaryRef),
		SafeSummary:            strings.TrimSpace(input.SafeSummary),
		PlanFingerprint:        strings.TrimSpace(input.PlanFingerprint),
		RiskProfileRef:         strings.TrimSpace(input.RiskProfileRef),
	}
	for _, ref := range []struct {
		name     string
		value    string
		required bool
	}{
		{name: "self_deploy_plan.ref", value: normalized.SelfDeployPlanRef, required: true},
		{name: "self_deploy_plan.provider_signal_ref", value: normalized.ProviderSignalRef},
		{name: "self_deploy_plan.source_ref", value: normalized.SourceRef},
		{name: "self_deploy_plan.merge_commit_sha", value: normalized.MergeCommitSHA},
		{name: "self_deploy_plan.services_yaml_ref", value: normalized.ServicesYAMLRef},
		{name: "self_deploy_plan.services_yaml_digest", value: normalized.ServicesYAMLDigest},
		{name: "self_deploy_plan.changed_files_summary_ref", value: normalized.ChangedFilesSummaryRef},
		{name: "self_deploy_plan.plan_fingerprint", value: normalized.PlanFingerprint, required: true},
		{name: "self_deploy_plan.risk_profile_ref", value: normalized.RiskProfileRef},
	} {
		if err := validateReleaseSafeRef(ref.name, ref.value, ref.required); err != nil {
			return normalizedSelfDeployPlanGateInput{}, err
		}
	}
	safeSummary, err := normalizeReleaseSafeText("self_deploy_plan.safe_summary", normalized.SafeSummary, maxEvaluationSummaryLength)
	if err != nil {
		return normalizedSelfDeployPlanGateInput{}, err
	}
	normalized.SafeSummary = safeSummary
	normalized.AffectedServiceKeys, err = normalizeSelfDeployTokens("self_deploy_plan.affected_service_key", input.AffectedServiceKeys)
	if err != nil {
		return normalizedSelfDeployPlanGateInput{}, err
	}
	normalized.PathCategories, err = normalizeSelfDeployTokens("self_deploy_plan.path_category", input.PathCategories)
	if err != nil {
		return normalizedSelfDeployPlanGateInput{}, err
	}
	normalized.ExpectedRuntimeJobTypes, err = normalizeSelfDeployTokens("self_deploy_plan.expected_runtime_job_type", input.ExpectedRuntimeJobTypes)
	if err != nil {
		return normalizedSelfDeployPlanGateInput{}, err
	}
	normalized.EvidenceRefs, err = normalizeEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return normalizedSelfDeployPlanGateInput{}, err
	}
	return normalized, nil
}

func normalizeSelfDeployTokens(name string, values []string) ([]string, error) {
	if len(values) > maxReleasePackageRefs {
		return nil, errs.ErrInvalidArgument
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		normalized := normalizeSelfDeployToken(value)
		if normalized == "" {
			continue
		}
		if err := validateReleaseSafeRef(name, normalized, true); err != nil {
			return nil, err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeSelfDeployToken(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{
		"self_deploy_path_category_",
		"path_category_",
		"job_type_",
		"runtime_job_type_",
	} {
		normalized = strings.TrimPrefix(normalized, prefix)
	}
	normalized = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(normalized)
	return normalized
}

func selfDeployPlanRiskSummary(input normalizedSelfDeployPlanGateInput) value.RiskEvaluationSummary {
	return value.RiskEvaluationSummary{
		ChangedFilesSummaryRef: input.ChangedFilesSummaryRef,
		Summary:                selfDeployPlanSafeSummary(input),
		Factors:                selfDeployPlanRiskFactors(input),
	}
}

func selfDeployPlanRiskFactors(input normalizedSelfDeployPlanGateInput) []value.RiskEvaluationFactor {
	factors := make([]value.RiskEvaluationFactor, 0, len(input.AffectedServiceKeys)+len(input.PathCategories)+len(input.ExpectedRuntimeJobTypes)+2)
	if input.ServicesYAMLRef != "" || input.ServicesYAMLDigest != "" {
		ref := input.ServicesYAMLRef
		if ref == "" {
			ref = "services-yaml:" + input.ServicesYAMLDigest
		}
		factors = append(factors, value.RiskEvaluationFactor{
			SourceType: string(enum.RiskFactorSourceTypePolicy),
			Ref:        ref,
			Summary:    "services.yaml projection changed",
			Tags:       []string{"services_yaml", "deploy_plan", "owner_approval"},
		})
	}
	for _, serviceKey := range input.AffectedServiceKeys {
		factors = append(factors, value.RiskEvaluationFactor{
			SourceType: string(enum.RiskFactorSourceTypeService),
			Ref:        "service:" + serviceKey,
			Summary:    "self-deploy affected service",
			Tags:       []string{"service", "self_deploy"},
		})
	}
	for _, category := range input.PathCategories {
		factors = append(factors, selfDeployPathCategoryFactor(category))
	}
	for _, jobType := range input.ExpectedRuntimeJobTypes {
		factors = append(factors, value.RiskEvaluationFactor{
			SourceType: string(enum.RiskFactorSourceTypeRuntime),
			Ref:        "runtime-job-type:" + jobType,
			Summary:    "self-deploy expected runtime job",
			Tags:       selfDeployRuntimeJobTags(jobType),
		})
	}
	if input.SourceRef != "" {
		factors = append(factors, value.RiskEvaluationFactor{
			SourceType: string(enum.RiskFactorSourceTypeRelease),
			Ref:        input.SourceRef,
			Summary:    "self-deploy source ref",
			Tags:       []string{"self_deploy"},
		})
	}
	return factors
}

func selfDeployPathCategoryFactor(category string) value.RiskEvaluationFactor {
	sourceType := enum.RiskFactorSourceTypeChangedFile
	tags := []string{"self_deploy", category}
	switch category {
	case "services_yaml", "services_yml", "service_descriptor":
		sourceType = enum.RiskFactorSourceTypePolicy
		tags = []string{"services_yaml", "deploy_plan", "owner_approval"}
	case "deploy_manifest", "kubernetes_manifest", "k8s_manifest", "rbac", "service_account":
		sourceType = enum.RiskFactorSourceTypeRuntime
		tags = []string{category, "deploy_plan", "owner_approval"}
	case "database", "db", "migration", "schema":
		sourceType = enum.RiskFactorSourceTypeDatabase
		tags = []string{category, "migration", "owner_approval"}
	case "secret", "secrets", "secret_value", "kubeconfig", "private_key", "auth_bypass", "disable_auth":
		sourceType = enum.RiskFactorSourceTypeSecret
		tags = []string{category, "secret_value"}
	case "runtime", "runner", "runtime_executor", "agent_runner", "gateway", "provider_write", "release_policy", "branch_rule":
		sourceType = enum.RiskFactorSourceTypeRuntime
		tags = []string{category, "deploy_plan", "owner_approval"}
	case "docs", "documentation", "readme", "test", "tests", "lint":
		tags = []string{category}
	default:
		tags = append(tags, "deploy_plan")
	}
	return value.RiskEvaluationFactor{
		SourceType: string(sourceType),
		Ref:        "path-category:" + category,
		Summary:    "self-deploy path category",
		Tags:       tags,
	}
}

func selfDeployRuntimeJobTags(jobType string) []string {
	switch jobType {
	case "deploy", "health_check", "postdeploy", "rollback":
		return []string{"deploy_plan", "runtime", "owner_approval", jobType}
	case "build", "image", "container":
		return []string{"deploy_plan", "image", "container", "owner_approval"}
	default:
		return []string{"deploy_plan", "runtime", "owner_approval"}
	}
}

func selfDeployPlanEvidenceRefs(input normalizedSelfDeployPlanGateInput) []value.EvidenceRef {
	refs := make([]value.EvidenceRef, 0, len(input.EvidenceRefs)+5)
	refs = append(refs, value.EvidenceRef{
		Kind:           selfDeployPlanEvidenceKind,
		Ref:            input.SelfDeployPlanRef,
		Summary:        selfDeployPlanGateEvidenceSummary,
		Digest:         input.PlanFingerprint,
		RetentionClass: "safe_ref",
	})
	refs = appendOptionalEvidenceRef(refs, "provider_signal", input.ProviderSignalRef, "self-deploy provider signal", "")
	refs = appendOptionalEvidenceRef(refs, "source_ref", input.SourceRef, "self-deploy source ref", "")
	refs = appendOptionalEvidenceRef(refs, "merge_commit", input.MergeCommitSHA, "self-deploy merge commit", "")
	refs = appendOptionalEvidenceRef(refs, "services_yaml", input.ServicesYAMLRef, "self-deploy services.yaml ref", input.ServicesYAMLDigest)
	refs = append(refs, input.EvidenceRefs...)
	return refs
}

func selfDeployPlanGateEvidenceRefs(input normalizedSelfDeployPlanGateInput) []value.EvidenceRef {
	return selfDeployPlanEvidenceRefs(input)
}

func appendOptionalEvidenceRef(refs []value.EvidenceRef, kind string, ref string, summary string, digest string) []value.EvidenceRef {
	if strings.TrimSpace(ref) == "" {
		return refs
	}
	return append(refs, value.EvidenceRef{Kind: kind, Ref: ref, Summary: summary, Digest: strings.TrimSpace(digest), RetentionClass: "safe_ref"})
}

func selfDeployPlanSafeSummary(input normalizedSelfDeployPlanGateInput) string {
	if input.SafeSummary != "" {
		return input.SafeSummary
	}
	return "self-deploy plan requires governance decision before runtime jobs"
}

func selfDeployPlanGateSummary(input normalizedSelfDeployPlanGateInput, assessment entity.RiskAssessment) string {
	summary := selfDeployPlanSafeSummary(input)
	return summary + "; risk_class=" + string(assessment.EffectiveRiskClass)
}

func selfDeployPlanTarget(planRef string) value.ExternalRef {
	return value.ExternalRef{Type: selfDeployPlanTargetType, Ref: strings.TrimSpace(planRef)}
}

func selfDeployAssessmentMatchesFingerprint(assessment entity.RiskAssessment, input normalizedSelfDeployPlanGateInput) bool {
	for _, ref := range assessment.EvidenceRefs {
		if strings.TrimSpace(ref.Kind) == selfDeployPlanEvidenceKind && strings.TrimSpace(ref.Ref) == input.SelfDeployPlanRef {
			return strings.TrimSpace(ref.Digest) == input.PlanFingerprint
		}
	}
	return false
}

func selectSelfDeployGateRequest(requests []entity.GateRequest) (entity.GateRequest, bool) {
	for _, request := range requests {
		if request.Status == enum.GateRequestStatusRequested || request.Status == enum.GateRequestStatusDelivering || request.Status == enum.GateRequestStatusAwaitingDecision {
			return request, true
		}
	}
	for _, request := range requests {
		if request.Status == enum.GateRequestStatusResolved {
			return request, true
		}
	}
	if len(requests) == 0 {
		return entity.GateRequest{}, false
	}
	return requests[0], true
}

func selfDeployGatePolicyRef(required []entity.RequiredGate) *uuid.UUID {
	if len(required) == 0 || required[0].GatePolicyID == uuid.Nil {
		return nil
	}
	gatePolicyID := required[0].GatePolicyID
	return &gatePolicyID
}

func selfDeployPlanGateStatus(assessment entity.RiskAssessment, request entity.GateRequest, decision *entity.GateDecision) enum.SelfDeployPlanGateStatus {
	if decision != nil {
		switch decision.Outcome {
		case enum.GateOutcomeApprove, enum.GateOutcomeApproveWithConditions:
			return enum.SelfDeployPlanGateStatusApproved
		case enum.GateOutcomeRevise:
			return enum.SelfDeployPlanGateStatusRequestChanges
		case enum.GateOutcomeReject:
			return enum.SelfDeployPlanGateStatusRejected
		default:
			return enum.SelfDeployPlanGateStatusBlocked
		}
	}
	switch request.Status {
	case enum.GateRequestStatusRequested, enum.GateRequestStatusDelivering, enum.GateRequestStatusAwaitingDecision:
		return enum.SelfDeployPlanGateStatusPending
	case enum.GateRequestStatusCancelled, enum.GateRequestStatusExpired, enum.GateRequestStatusResolved:
		return enum.SelfDeployPlanGateStatusBlocked
	}
	if riskClassAtLeast(assessment.EffectiveRiskClass, enum.RiskClassR2) || len(assessment.RequiredGates) > 0 {
		return enum.SelfDeployPlanGateStatusPending
	}
	return enum.SelfDeployPlanGateStatusApproved
}

func derivedSelfDeployCommandMeta(meta CommandMeta, step string, fingerprint string) CommandMeta {
	return CommandMeta{
		IdempotencyKey: "self-deploy-plan-gate:" + strings.TrimSpace(fingerprint) + ":" + step,
		Actor:          meta.Actor,
		Reason:         meta.Reason,
		RequestID:      meta.RequestID,
		RequestContext: meta.RequestContext,
	}
}

func queryMetaFromCommandMeta(meta CommandMeta) QueryMeta {
	return QueryMeta{Actor: meta.Actor, RequestID: meta.RequestID, RequestContext: meta.RequestContext}
}
