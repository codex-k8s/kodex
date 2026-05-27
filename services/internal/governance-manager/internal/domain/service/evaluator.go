package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/google/uuid"

	governanceevents "github.com/codex-k8s/kodex/libs/go/platformevents/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

const (
	maxEvaluationSummaryLength = 2048
	maxEvaluationFactorSummary = 512
	maxEvaluationRefLength     = 512
	maxEvaluationTagLength     = 64
	maxEvaluationTags          = 32
	maxEvaluationFactors       = 64
)

type evaluationPolicy struct {
	riskProfileID      *uuid.UUID
	riskProfileVersion *int64
	rules              []entity.RiskRule
	gatePolicies       []entity.GatePolicy
}

type evaluationContext struct {
	target   value.ExternalRef
	project  value.ProjectContextRef
	provider providerEvaluationContext
	agent    agentEvaluationContext
	runtime  runtimeEvaluationContext
	summary  value.RiskEvaluationSummary
}

type providerEvaluationContext struct {
	WorkItemRef            string `json:"work_item_ref,omitempty"`
	PullRequestRef         string `json:"pull_request_ref,omitempty"`
	ReviewSignalRef        string `json:"review_signal_ref,omitempty"`
	ProviderOperationRef   string `json:"provider_operation_ref,omitempty"`
	ChangedFilesSummaryRef string `json:"changed_files_summary_ref,omitempty"`
}

type agentEvaluationContext struct {
	SessionRef    string `json:"session_ref,omitempty"`
	RunRef        string `json:"run_ref,omitempty"`
	StageRef      string `json:"stage_ref,omitempty"`
	AcceptanceRef string `json:"acceptance_ref,omitempty"`
	RoleRef       string `json:"role_ref,omitempty"`
}

type runtimeEvaluationContext struct {
	SlotRef        string `json:"slot_ref,omitempty"`
	JobRef         string `json:"job_ref,omitempty"`
	EnvironmentRef string `json:"environment_ref,omitempty"`
	ArtifactRef    string `json:"artifact_ref,omitempty"`
	SummaryRef     string `json:"summary_ref,omitempty"`
}

type riskRuleMatcher struct {
	TargetType             string   `json:"target_type,omitempty"`
	TargetRef              string   `json:"target_ref,omitempty"`
	ProjectRef             string   `json:"project_ref,omitempty"`
	RepositoryRef          string   `json:"repository_ref,omitempty"`
	ServiceRef             string   `json:"service_ref,omitempty"`
	Service                string   `json:"service,omitempty"`
	BranchRulesRef         string   `json:"branch_rules_ref,omitempty"`
	ReleasePolicyRef       string   `json:"release_policy_ref,omitempty"`
	ReleaseLineRef         string   `json:"release_line_ref,omitempty"`
	ReleaseLine            string   `json:"release_line,omitempty"`
	ProviderWorkItemRef    string   `json:"provider_work_item_ref,omitempty"`
	ProviderPullRequestRef string   `json:"provider_pull_request_ref,omitempty"`
	ChangedFilesSummaryRef string   `json:"changed_files_summary_ref,omitempty"`
	AgentRunRef            string   `json:"agent_run_ref,omitempty"`
	RuntimeJobRef          string   `json:"runtime_job_ref,omitempty"`
	RuntimeEnvironmentRef  string   `json:"runtime_environment_ref,omitempty"`
	FactorSourceType       string   `json:"factor_source_type,omitempty"`
	FactorRef              string   `json:"factor_ref,omitempty"`
	FactorTag              string   `json:"factor_tag,omitempty"`
	Tag                    string   `json:"tag,omitempty"`
	Tags                   []string `json:"tags,omitempty"`
	PathGlob               string   `json:"path_glob,omitempty"`
	RefContains            string   `json:"ref_contains,omitempty"`
	SummaryContains        string   `json:"summary_contains,omitempty"`
}

func (s *Service) evaluateRisk(ctx context.Context, input EvaluateRiskInput) (entity.RiskAssessment, error) {
	if input.Target.Type == "" || input.Target.Ref == "" {
		return entity.RiskAssessment{}, errs.ErrInvalidArgument
	}
	if err := requireCommand(input.Meta, enum.OperationEvaluateRisk.String()); err != nil {
		return entity.RiskAssessment{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionRiskEvaluate, riskTargetResource(input.Target)); err != nil {
		return entity.RiskAssessment{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationEvaluateRisk.String(), governanceevents.AggregateRiskAssessment)
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	if replayed {
		assessment, err := s.repository.GetRiskAssessment(ctx, result.AggregateID)
		if err != nil {
			return entity.RiskAssessment{}, err
		}
		if !sameExternalRef(assessment.Target, input.Target) {
			return entity.RiskAssessment{}, errs.ErrConflict
		}
		return assessment, nil
	}
	summary, err := normalizeRiskEvaluationSummary(input.EvaluationSummary)
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	evidenceRefs, err := normalizeEvidenceRefs(input.EvidenceRefs)
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	policy, err := s.resolveActiveEvaluationPolicy(ctx, input.RiskProfileRef)
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	evaluation, err := s.classifyRisk(ctx, evaluationInput{
		assessmentID:    s.idGenerator.New(),
		target:          input.Target,
		projectContext:  input.ProjectContext,
		providerContext: input.ProviderContext,
		agentContext:    input.AgentContext,
		runtimeContext:  input.RuntimeContext,
		summary:         summary,
		policy:          policy,
	})
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	summary = evaluation.context.summary
	now := s.clock.Now()
	assessment := entity.RiskAssessment{
		VersionedBase:      entity.VersionedBase{ID: evaluation.assessmentID, Version: 1, CreatedAt: now, UpdatedAt: now},
		Target:             input.Target,
		ProjectContext:     input.ProjectContext,
		ProviderContext:    input.ProviderContext,
		AgentContext:       input.AgentContext,
		RuntimeContext:     input.RuntimeContext,
		RiskProfileID:      policy.riskProfileID,
		RiskProfileVersion: policy.riskProfileVersion,
		EvaluationSummary:  summary,
		EvidenceRefs:       evidenceRefs,
		InitialRiskClass:   evaluation.riskClass,
		EffectiveRiskClass: evaluation.riskClass,
		Status:             enum.RiskAssessmentStatusActive,
		Explanation:        evaluation.explanation,
		RequiredGates:      evaluation.requiredGates,
	}
	factors := evaluation.factors
	for index := range factors {
		factors[index].CreatedAt = now
	}
	result = commandResult(input.Meta, enum.OperationEvaluateRisk.String(), governanceevents.AggregateRiskAssessment, assessment.ID, now)
	events := []entity.OutboxEvent{
		outboxCommandEvent(s.idGenerator.New(), governanceevents.EventRiskAssessmentRequested, governanceevents.AggregateRiskAssessment, assessment.ID, now, input.Meta, enum.OperationEvaluateRisk.String(), riskAssessmentEventRefs(governanceevents.Payload{
			RiskAssessmentID: assessment.ID.String(),
			SafeSummary:      assessment.Explanation,
			Status:           string(assessment.Status),
			Version:          assessment.Version,
		}, assessment, evaluation.context)),
		outboxCommandEvent(s.idGenerator.New(), governanceevents.EventRiskAssessmentCompleted, governanceevents.AggregateRiskAssessment, assessment.ID, now, input.Meta, enum.OperationEvaluateRisk.String(), riskAssessmentCompletedPayload(assessment, evaluation.context, len(factors))),
	}
	if err := s.repository.CreateRiskAssessment(ctx, assessment, factors, result, events); err != nil {
		return entity.RiskAssessment{}, err
	}
	return assessment, nil
}

func (s *Service) reevaluateRisk(ctx context.Context, input ReevaluateRiskInput) (entity.RiskAssessment, error) {
	if input.RiskAssessmentID == uuid.Nil {
		return entity.RiskAssessment{}, errs.ErrInvalidArgument
	}
	if err := requireCommand(input.Meta, enum.OperationReevaluateRisk.String()); err != nil {
		return entity.RiskAssessment{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionRiskEvaluate, riskAssessmentResource(input.RiskAssessmentID)); err != nil {
		return entity.RiskAssessment{}, err
	}
	result, replayed, err := s.replayCommand(ctx, input.Meta, enum.OperationReevaluateRisk.String(), governanceevents.AggregateRiskAssessment)
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	if replayed {
		if result.AggregateID != input.RiskAssessmentID {
			return entity.RiskAssessment{}, errs.ErrConflict
		}
		return s.repository.GetRiskAssessment(ctx, result.AggregateID)
	}
	previousVersion, err := requireExpectedVersion(input.Meta)
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	assessment, err := s.repository.GetRiskAssessment(ctx, input.RiskAssessmentID)
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	if assessment.Version != previousVersion {
		return entity.RiskAssessment{}, errs.ErrConflict
	}
	previousFactors, _, err := s.repository.ListRiskFactors(ctx, query.RiskFactorFilter{RiskAssessmentID: assessment.ID})
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	previousOutcomeSignature := riskAssessmentOutcomeSignature(previousFactors, assessment.RequiredGates, assessment.EvidenceRefs)
	summary := assessment.EvaluationSummary
	if riskEvaluationSummaryProvided(input.EvaluationSummary) {
		summary, err = normalizeRiskEvaluationSummary(input.EvaluationSummary)
		if err != nil {
			return entity.RiskAssessment{}, err
		}
	}
	evidenceRefs, err := normalizeEvidenceRefs(append(assessment.EvidenceRefs, input.NewEvidenceRefs...))
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	policy, err := s.resolveStoredEvaluationPolicy(ctx, assessment, input.RiskProfileRef)
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	evaluation, err := s.classifyRisk(ctx, evaluationInput{
		assessmentID:    assessment.ID,
		target:          assessment.Target,
		projectContext:  assessment.ProjectContext,
		providerContext: assessment.ProviderContext,
		agentContext:    assessment.AgentContext,
		runtimeContext:  assessment.RuntimeContext,
		summary:         summary,
		policy:          policy,
		includeSignals:  true,
	})
	if err != nil {
		return entity.RiskAssessment{}, err
	}
	now := s.clock.Now()
	previousEffectiveRiskClass := assessment.EffectiveRiskClass
	previousRequiredGateCount := len(assessment.RequiredGates)
	assessment.Version = previousVersion + 1
	assessment.UpdatedAt = now
	assessment.RiskProfileID = policy.riskProfileID
	assessment.RiskProfileVersion = policy.riskProfileVersion
	assessment.EvaluationSummary = evaluation.context.summary
	assessment.EvidenceRefs = evidenceRefs
	assessment.InitialRiskClass = evaluation.riskClass
	assessment.EffectiveRiskClass = evaluation.riskClass
	assessment.Status = enum.RiskAssessmentStatusActive
	assessment.Explanation = evaluation.explanation
	assessment.RequiredGates = evaluation.requiredGates
	factors := evaluation.factors
	for index := range factors {
		factors[index].CreatedAt = now
	}
	result = commandResult(input.Meta, enum.OperationReevaluateRisk.String(), governanceevents.AggregateRiskAssessment, assessment.ID, now)
	events := []entity.OutboxEvent{
		outboxCommandEvent(s.idGenerator.New(), governanceevents.EventRiskAssessmentCompleted, governanceevents.AggregateRiskAssessment, assessment.ID, now, input.Meta, enum.OperationReevaluateRisk.String(), riskAssessmentCompletedPayload(assessment, evaluation.context, len(factors))),
	}
	currentOutcomeSignature := riskAssessmentOutcomeSignature(factors, assessment.RequiredGates, assessment.EvidenceRefs)
	if previousEffectiveRiskClass != assessment.EffectiveRiskClass || previousRequiredGateCount != len(assessment.RequiredGates) || previousOutcomeSignature != currentOutcomeSignature {
		events = append(events, outboxCommandEvent(s.idGenerator.New(), governanceevents.EventRiskAssessmentChanged, governanceevents.AggregateRiskAssessment, assessment.ID, now, input.Meta, enum.OperationReevaluateRisk.String(), riskAssessmentEventRefs(governanceevents.Payload{
			RiskAssessmentID:           assessment.ID.String(),
			PreviousEffectiveRiskClass: string(previousEffectiveRiskClass),
			EffectiveRiskClass:         string(assessment.EffectiveRiskClass),
			RiskFactorCount:            int64(len(factors)),
			RequiredGateCount:          int64(len(assessment.RequiredGates)),
			SafeSummary:                assessment.Explanation,
			Status:                     string(assessment.Status),
			Version:                    assessment.Version,
		}, assessment, evaluation.context)))
	}
	if err := s.repository.UpdateRiskAssessment(ctx, assessment, factors, previousVersion, result, events); err != nil {
		return entity.RiskAssessment{}, err
	}
	return assessment, nil
}

func riskAssessmentCompletedPayload(assessment entity.RiskAssessment, context evaluationContext, factorCount int) governanceevents.Payload {
	return riskAssessmentEventRefs(governanceevents.Payload{
		RiskAssessmentID:   assessment.ID.String(),
		InitialRiskClass:   string(assessment.InitialRiskClass),
		EffectiveRiskClass: string(assessment.EffectiveRiskClass),
		RiskFactorCount:    int64(factorCount),
		RequiredGateCount:  int64(len(assessment.RequiredGates)),
		SafeSummary:        assessment.Explanation,
		Status:             string(assessment.Status),
		Version:            assessment.Version,
	}, assessment, context)
}

func riskAssessmentEventRefs(payload governanceevents.Payload, assessment entity.RiskAssessment, context evaluationContext) governanceevents.Payload {
	payload = applyTargetRef(payload, assessment.Target)
	payload = applyProjectContextRefs(payload, assessment.ProjectContext)
	payload.ProviderWorkItemRef = context.provider.WorkItemRef
	payload.ProviderPullRequestRef = context.provider.PullRequestRef
	payload.AgentSessionRef = context.agent.SessionRef
	payload.AgentRunRef = context.agent.RunRef
	payload.AgentStageRef = context.agent.StageRef
	payload.RuntimeJobRef = context.runtime.JobRef
	return payload
}

func riskAssessmentOutcomeSignature(factors []entity.RiskFactor, gates []entity.RequiredGate, evidenceRefs []value.EvidenceRef) string {
	factorItems := make([]riskFactorSignature, 0, len(factors))
	for _, factor := range factors {
		factorItems = append(factorItems, riskFactorSignature{
			SourceType: string(factor.SourceType),
			SourceRef:  strings.TrimSpace(factor.SourceRef),
			RiskClass:  string(factor.RiskClass),
			Summary:    strings.TrimSpace(factor.Summary),
		})
	}
	slices.SortFunc(factorItems, func(left, right riskFactorSignature) int {
		return strings.Compare(left.key(), right.key())
	})

	gateItems := make([]requiredGateSignature, 0, len(gates))
	for _, gate := range gates {
		gateItems = append(gateItems, requiredGateSignature{
			GatePolicyID: gate.GatePolicyID.String(),
			GateKind:     string(gate.GateKind),
			MinRiskClass: string(gate.MinRiskClass),
			Reason:       strings.TrimSpace(gate.Reason),
		})
	}
	slices.SortFunc(gateItems, func(left, right requiredGateSignature) int {
		return strings.Compare(left.key(), right.key())
	})

	evidenceItems := make([]evidenceRefSignature, 0, len(evidenceRefs))
	for _, ref := range evidenceRefs {
		evidenceItems = append(evidenceItems, evidenceRefSignature{
			Kind:           strings.TrimSpace(ref.Kind),
			Ref:            strings.TrimSpace(ref.Ref),
			Summary:        strings.TrimSpace(ref.Summary),
			Digest:         strings.TrimSpace(ref.Digest),
			RetentionClass: strings.TrimSpace(ref.RetentionClass),
		})
	}
	slices.SortFunc(evidenceItems, func(left, right evidenceRefSignature) int {
		return strings.Compare(left.key(), right.key())
	})

	payload, _ := json.Marshal(struct {
		Factors  []riskFactorSignature   `json:"factors"`
		Gates    []requiredGateSignature `json:"gates"`
		Evidence []evidenceRefSignature  `json:"evidence"`
	}{Factors: factorItems, Gates: gateItems, Evidence: evidenceItems})
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum[:])
}

type riskFactorSignature struct {
	SourceType string `json:"source_type"`
	SourceRef  string `json:"source_ref"`
	RiskClass  string `json:"risk_class"`
	Summary    string `json:"summary"`
}

func (item riskFactorSignature) key() string {
	return item.SourceType + "\x00" + item.SourceRef + "\x00" + item.RiskClass + "\x00" + item.Summary
}

type requiredGateSignature struct {
	GatePolicyID string `json:"gate_policy_id"`
	GateKind     string `json:"gate_kind"`
	MinRiskClass string `json:"min_risk_class"`
	Reason       string `json:"reason"`
}

func (item requiredGateSignature) key() string {
	return item.GatePolicyID + "\x00" + item.GateKind + "\x00" + item.MinRiskClass + "\x00" + item.Reason
}

type evidenceRefSignature struct {
	Kind           string `json:"kind"`
	Ref            string `json:"ref"`
	Summary        string `json:"summary"`
	Digest         string `json:"digest"`
	RetentionClass string `json:"retention_class"`
}

func (item evidenceRefSignature) key() string {
	return item.Kind + "\x00" + item.Ref + "\x00" + item.Summary + "\x00" + item.Digest + "\x00" + item.RetentionClass
}

type evaluationInput struct {
	assessmentID    uuid.UUID
	target          value.ExternalRef
	projectContext  value.ProjectContextRef
	providerContext []byte
	agentContext    []byte
	runtimeContext  []byte
	summary         value.RiskEvaluationSummary
	policy          evaluationPolicy
	includeSignals  bool
}

type evaluationOutput struct {
	assessmentID  uuid.UUID
	context       evaluationContext
	factors       []entity.RiskFactor
	riskClass     enum.RiskClass
	requiredGates []entity.RequiredGate
	explanation   string
}

func (s *Service) classifyRisk(ctx context.Context, input evaluationInput) (evaluationOutput, error) {
	contextValue, err := evaluationContextFromInput(input)
	if err != nil {
		return evaluationOutput{}, err
	}
	factors := s.inputRiskFactors(input.assessmentID, contextValue.summary)
	policyFactors, err := s.policyRiskFactors(input.assessmentID, contextValue, input.policy)
	if err != nil {
		return evaluationOutput{}, err
	}
	factors = append(factors, policyFactors...)
	if input.includeSignals {
		signalFactors, err := s.reviewSignalFactors(ctx, input.assessmentID)
		if err != nil {
			return evaluationOutput{}, err
		}
		factors = append(factors, signalFactors...)
	}
	effectiveRisk := enum.RiskClassR0
	for _, factor := range factors {
		effectiveRisk = maxRiskClass(effectiveRisk, factor.RiskClass)
	}
	requiredGates, err := requiredGatesForRisk(effectiveRisk, input.policy, factors)
	if err != nil {
		return evaluationOutput{}, err
	}
	return evaluationOutput{
		assessmentID:  input.assessmentID,
		context:       contextValue,
		factors:       factors,
		riskClass:     effectiveRisk,
		requiredGates: requiredGates,
		explanation:   riskExplanation(effectiveRisk, len(factors), len(requiredGates)),
	}, nil
}

func evaluationContextFromInput(input evaluationInput) (evaluationContext, error) {
	var provider providerEvaluationContext
	if err := unmarshalOptionalJSON(input.providerContext, &provider); err != nil {
		return evaluationContext{}, err
	}
	if input.summary.ChangedFilesSummaryRef == "" {
		changedFilesSummaryRef := strings.TrimSpace(provider.ChangedFilesSummaryRef)
		if err := validateSafeRef("provider_context.changed_files_summary_ref", changedFilesSummaryRef, false); err != nil {
			return evaluationContext{}, err
		}
		input.summary.ChangedFilesSummaryRef = changedFilesSummaryRef
	}
	var agent agentEvaluationContext
	if err := unmarshalOptionalJSON(input.agentContext, &agent); err != nil {
		return evaluationContext{}, err
	}
	var runtime runtimeEvaluationContext
	if err := unmarshalOptionalJSON(input.runtimeContext, &runtime); err != nil {
		return evaluationContext{}, err
	}
	return evaluationContext{target: input.target, project: input.projectContext, provider: provider, agent: agent, runtime: runtime, summary: input.summary}, nil
}

func unmarshalOptionalJSON[T any](payload []byte, target *T) error {
	if len(payload) == 0 || string(payload) == "null" {
		return nil
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("%w: invalid evaluation context", errs.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) inputRiskFactors(assessmentID uuid.UUID, summary value.RiskEvaluationSummary) []entity.RiskFactor {
	factors := make([]entity.RiskFactor, 0, len(summary.Factors))
	for _, factor := range summary.Factors {
		sourceType := enum.RiskFactorSourceType(factor.SourceType)
		riskClass := defaultRiskForInputFactor(factor)
		factors = append(factors, entity.RiskFactor{
			ID:               s.idGenerator.New(),
			RiskAssessmentID: assessmentID,
			SourceType:       sourceType,
			SourceRef:        strings.TrimSpace(factor.Ref),
			RiskClass:        riskClass,
			Summary:          strings.TrimSpace(factor.Summary),
		})
	}
	if summary.ChangedFilesSummaryRef != "" && !hasFactorRef(factors, enum.RiskFactorSourceTypeChangedFile, summary.ChangedFilesSummaryRef) {
		factors = append(factors, entity.RiskFactor{
			ID:               s.idGenerator.New(),
			RiskAssessmentID: assessmentID,
			SourceType:       enum.RiskFactorSourceTypeChangedFile,
			SourceRef:        summary.ChangedFilesSummaryRef,
			RiskClass:        enum.RiskClassR1,
			Summary:          summary.Summary,
		})
	}
	return factors
}

func (s *Service) policyRiskFactors(assessmentID uuid.UUID, contextValue evaluationContext, policy evaluationPolicy) ([]entity.RiskFactor, error) {
	factors := make([]entity.RiskFactor, 0, len(policy.rules))
	inputFactors := contextValue.summary.Factors
	for _, rule := range policy.rules {
		if rule.Status != enum.RuleStatusActive {
			continue
		}
		matched, err := riskRuleMatches(rule, contextValue, inputFactors)
		if err != nil {
			return nil, err
		}
		if !matched {
			continue
		}
		factors = append(factors, entity.RiskFactor{
			ID:               s.idGenerator.New(),
			RiskAssessmentID: assessmentID,
			SourceType:       enum.RiskFactorSourceTypePolicy,
			SourceRef:        rule.ID.String(),
			RiskClass:        rule.MinRiskClass,
			Summary:          ruleReason(rule),
		})
	}
	return factors, nil
}

func (s *Service) reviewSignalFactors(ctx context.Context, assessmentID uuid.UUID) ([]entity.RiskFactor, error) {
	signals, _, err := s.repository.ListReviewSignals(ctx, query.ReviewSignalFilter{RiskAssessmentID: &assessmentID})
	if err != nil {
		return nil, err
	}
	factors := make([]entity.RiskFactor, 0, len(signals))
	for _, signal := range signals {
		riskClass := riskForReviewSignal(signal)
		if riskClass == enum.RiskClassR0 {
			continue
		}
		factors = append(factors, entity.RiskFactor{
			ID:               s.idGenerator.New(),
			RiskAssessmentID: assessmentID,
			SourceType:       enum.RiskFactorSourceTypeReviewSignal,
			SourceRef:        signal.ID.String(),
			RiskClass:        riskClass,
			Summary:          strings.TrimSpace(signal.Summary),
		})
	}
	return factors, nil
}

func riskRuleMatches(rule entity.RiskRule, contextValue evaluationContext, factors []value.RiskEvaluationFactor) (bool, error) {
	var matcher riskRuleMatcher
	if err := json.Unmarshal(rule.MatcherJSON, &matcher); err != nil {
		return false, fmt.Errorf("%w: invalid risk rule matcher", errs.ErrInvalidArgument)
	}
	matched, err := matcherMatches(matcher, contextValue, nil)
	if err != nil {
		return false, err
	}
	if matched {
		return true, nil
	}
	for index := range factors {
		matched, err := matcherMatches(matcher, contextValue, &factors[index])
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func matcherMatches(matcher riskRuleMatcher, contextValue evaluationContext, factor *value.RiskEvaluationFactor) (bool, error) {
	if !matchesString(matcher.TargetType, contextValue.target.Type) ||
		!matchesString(matcher.TargetRef, contextValue.target.Ref) ||
		!matchesString(matcher.ProjectRef, contextValue.project.ProjectRef) ||
		!matchesString(matcher.RepositoryRef, contextValue.project.RepositoryRef) ||
		!matchesAnyString(firstNonEmpty(matcher.ServiceRef, matcher.Service), contextValue.project.ServiceRef, factorRef(factor)) ||
		!matchesString(matcher.BranchRulesRef, contextValue.project.BranchRulesRef) ||
		!matchesAnyString(firstNonEmpty(matcher.ReleaseLineRef, matcher.ReleaseLine), contextValue.project.ReleaseLineRef, factorRef(factor)) ||
		!matchesString(matcher.ReleasePolicyRef, contextValue.project.ReleasePolicyRef) ||
		!matchesString(matcher.ProviderWorkItemRef, contextValue.provider.WorkItemRef) ||
		!matchesString(matcher.ProviderPullRequestRef, contextValue.provider.PullRequestRef) ||
		!matchesString(matcher.ChangedFilesSummaryRef, contextValue.summary.ChangedFilesSummaryRef) ||
		!matchesString(matcher.AgentRunRef, contextValue.agent.RunRef) ||
		!matchesString(matcher.RuntimeJobRef, contextValue.runtime.JobRef) ||
		!matchesString(matcher.RuntimeEnvironmentRef, contextValue.runtime.EnvironmentRef) {
		return false, nil
	}
	if factor == nil {
		return matcher.FactorSourceType == "" && matcher.FactorRef == "" && matcher.FactorTag == "" && matcher.Tag == "" &&
			len(matcher.Tags) == 0 && matcher.PathGlob == "" && matcher.RefContains == "" && matcher.SummaryContains == "", nil
	}
	globMatched, err := matchesGlob(matcher.PathGlob, factor.Ref)
	if err != nil {
		return false, err
	}
	if !matchesString(matcher.FactorSourceType, factor.SourceType) ||
		!matchesString(matcher.FactorRef, factor.Ref) ||
		!matchesTag(firstNonEmpty(matcher.FactorTag, matcher.Tag), factor.Tags) ||
		!matchesTags(matcher.Tags, factor.Tags) ||
		!globMatched ||
		!containsString(factor.Ref, matcher.RefContains) ||
		!containsAnyString(matcher.SummaryContains, contextValue.summary.Summary, factor.Summary) {
		return false, nil
	}
	return true, nil
}

func requiredGatesForRisk(riskClass enum.RiskClass, policy evaluationPolicy, factors []entity.RiskFactor) ([]entity.RequiredGate, error) {
	required := make([]entity.RequiredGate, 0)
	seen := make(map[uuid.UUID]struct{})
	policiesByID := make(map[uuid.UUID]entity.GatePolicy, len(policy.gatePolicies))
	for _, gatePolicy := range policy.gatePolicies {
		if gatePolicy.Status == enum.RuleStatusActive {
			policiesByID[gatePolicy.ID] = gatePolicy
		}
	}
	for _, factor := range factors {
		if factor.SourceType != enum.RiskFactorSourceTypePolicy {
			continue
		}
		ruleID, err := uuid.Parse(factor.SourceRef)
		if err != nil {
			continue
		}
		for _, rule := range policy.rules {
			if rule.ID != ruleID || rule.RequiredGatePolicyID == nil {
				continue
			}
			gatePolicy, ok := policiesByID[*rule.RequiredGatePolicyID]
			if !ok {
				return nil, errs.ErrPreconditionFailed
			}
			addRequiredGate(&required, seen, gatePolicy, factor.Summary)
		}
	}
	for _, gatePolicy := range policiesByID {
		if riskClassAtLeast(riskClass, gatePolicy.MinRiskClass) {
			addRequiredGate(&required, seen, gatePolicy, "risk class requires gate policy")
		}
	}
	return required, nil
}

func addRequiredGate(required *[]entity.RequiredGate, seen map[uuid.UUID]struct{}, policy entity.GatePolicy, reason string) {
	if _, ok := seen[policy.ID]; ok {
		return
	}
	seen[policy.ID] = struct{}{}
	*required = append(*required, entity.RequiredGate{GatePolicyID: policy.ID, GateKind: policy.GateKind, MinRiskClass: policy.MinRiskClass, Reason: strings.TrimSpace(reason)})
}

func (s *Service) resolveActiveEvaluationPolicy(ctx context.Context, riskProfileRef string) (evaluationPolicy, error) {
	riskProfileID, err := parseRiskProfileRef(riskProfileRef)
	if err != nil || riskProfileID == nil {
		return evaluationPolicy{}, err
	}
	profile, err := s.repository.GetRiskProfile(ctx, *riskProfileID)
	if err != nil {
		return evaluationPolicy{}, err
	}
	if profile.ActiveVersion == nil || profile.Status != enum.RiskProfileStatusActive {
		return evaluationPolicy{}, errs.ErrPreconditionFailed
	}
	version, err := s.repository.GetRiskProfileVersion(ctx, profile.ID, *profile.ActiveVersion)
	if err != nil {
		return evaluationPolicy{}, err
	}
	if version.Status != enum.RiskProfileVersionStatusActive {
		return evaluationPolicy{}, errs.ErrPreconditionFailed
	}
	return evaluationPolicy{riskProfileID: &profile.ID, riskProfileVersion: profile.ActiveVersion, rules: version.Rules, gatePolicies: version.GatePolicies}, nil
}

func (s *Service) resolveStoredEvaluationPolicy(ctx context.Context, assessment entity.RiskAssessment, riskProfileRef string) (evaluationPolicy, error) {
	if strings.TrimSpace(riskProfileRef) != "" {
		return s.resolveActiveEvaluationPolicy(ctx, riskProfileRef)
	}
	if assessment.RiskProfileID == nil || assessment.RiskProfileVersion == nil {
		return evaluationPolicy{}, nil
	}
	version, err := s.repository.GetRiskProfileVersion(ctx, *assessment.RiskProfileID, *assessment.RiskProfileVersion)
	if err != nil {
		return evaluationPolicy{}, err
	}
	return evaluationPolicy{riskProfileID: assessment.RiskProfileID, riskProfileVersion: assessment.RiskProfileVersion, rules: version.Rules, gatePolicies: version.GatePolicies}, nil
}

func parseRiskProfileRef(riskProfileRef string) (*uuid.UUID, error) {
	value := strings.TrimSpace(riskProfileRef)
	if value == "" {
		return nil, nil
	}
	if after, ok := strings.CutPrefix(value, "risk_profile:"); ok {
		value = after
	}
	if after, ok := strings.CutPrefix(value, "governance:risk_profile:"); ok {
		value = after
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return &id, nil
}

func normalizeRiskEvaluationSummary(summary value.RiskEvaluationSummary) (value.RiskEvaluationSummary, error) {
	normalized := value.RiskEvaluationSummary{
		ChangedFilesSummaryRef: strings.TrimSpace(summary.ChangedFilesSummaryRef),
		Summary:                strings.TrimSpace(summary.Summary),
		Factors:                make([]value.RiskEvaluationFactor, 0, len(summary.Factors)),
	}
	if err := validateSafeText("evaluation_summary.summary", normalized.Summary, maxEvaluationSummaryLength); err != nil {
		return value.RiskEvaluationSummary{}, err
	}
	if err := validateSafeRef("evaluation_summary.changed_files_summary_ref", normalized.ChangedFilesSummaryRef, false); err != nil {
		return value.RiskEvaluationSummary{}, err
	}
	if len(summary.Factors) > maxEvaluationFactors {
		return value.RiskEvaluationSummary{}, errs.ErrInvalidArgument
	}
	for _, factor := range summary.Factors {
		sourceType := enum.RiskFactorSourceType(strings.TrimSpace(factor.SourceType))
		if !validRiskFactorSourceType(sourceType) {
			return value.RiskEvaluationSummary{}, errs.ErrInvalidArgument
		}
		normalizedFactor := value.RiskEvaluationFactor{
			SourceType: string(sourceType),
			Ref:        strings.TrimSpace(factor.Ref),
			Summary:    strings.TrimSpace(factor.Summary),
			Tags:       normalizeTags(factor.Tags),
		}
		if err := validateSafeRef("evaluation_factor.ref", normalizedFactor.Ref, true); err != nil {
			return value.RiskEvaluationSummary{}, err
		}
		if err := validateSafeText("evaluation_factor.summary", normalizedFactor.Summary, maxEvaluationFactorSummary); err != nil {
			return value.RiskEvaluationSummary{}, err
		}
		normalized.Factors = append(normalized.Factors, normalizedFactor)
	}
	return normalized, nil
}

func normalizeEvidenceRefs(refs []value.EvidenceRef) ([]value.EvidenceRef, error) {
	result := make([]value.EvidenceRef, 0, len(refs))
	seen := make(map[string]struct{})
	for _, ref := range refs {
		normalized, err := normalizeEvidenceRef(ref, "evidence_ref.ref", "evidence_ref.summary")
		if err != nil {
			return nil, err
		}
		key := normalized.Kind + "\x00" + normalized.Ref
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeEvidenceRef(ref value.EvidenceRef, refName string, summaryName string) (value.EvidenceRef, error) {
	normalized := trimEvidenceRef(ref)
	if normalized.Kind == "" || normalized.Ref == "" {
		return value.EvidenceRef{}, errs.ErrInvalidArgument
	}
	if err := validateSafeRef(refName, normalized.Ref, true); err != nil {
		return value.EvidenceRef{}, err
	}
	if err := validateSafeText(summaryName, normalized.Summary, maxEvaluationFactorSummary); err != nil {
		return value.EvidenceRef{}, err
	}
	return normalized, nil
}

func trimEvidenceRef(ref value.EvidenceRef) value.EvidenceRef {
	return value.EvidenceRef{
		Kind:           strings.TrimSpace(ref.Kind),
		Ref:            strings.TrimSpace(ref.Ref),
		Summary:        strings.TrimSpace(ref.Summary),
		Digest:         strings.TrimSpace(ref.Digest),
		RetentionClass: strings.TrimSpace(ref.RetentionClass),
	}
}

func validateSafeRef(_ string, value string, required bool) error {
	if strings.TrimSpace(value) == "" {
		if required {
			return errs.ErrInvalidArgument
		}
		return nil
	}
	if len(value) > maxEvaluationRefLength || strings.ContainsAny(value, "{}\n\r\t") || unsafeEvaluationText(value) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validateSafeText(_ string, value string, maxLength int) error {
	if len(value) > maxLength || unsafeEvaluationText(value) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func unsafeEvaluationText(value string) bool {
	normalized := strings.ToLower(value)
	for _, marker := range []string{"raw_provider_payload", "raw_diff", "raw_report", "raw logs", "token=", "password=", "begin private key"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func normalizeTags(tags []string) []string {
	result := make([]string, 0, min(len(tags), maxEvaluationTags))
	for _, tag := range tags {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		if normalized == "" || len(normalized) > maxEvaluationTagLength || unsafeEvaluationText(normalized) || slices.Contains(result, normalized) {
			continue
		}
		result = append(result, normalized)
		if len(result) == maxEvaluationTags {
			break
		}
	}
	return result
}

func defaultRiskForInputFactor(factor value.RiskEvaluationFactor) enum.RiskClass {
	riskClass := defaultRiskForSourceType(enum.RiskFactorSourceType(factor.SourceType))
	for _, tag := range factor.Tags {
		riskClass = maxRiskClass(riskClass, riskForTag(tag))
	}
	return riskClass
}

func defaultRiskForSourceType(sourceType enum.RiskFactorSourceType) enum.RiskClass {
	switch sourceType {
	case enum.RiskFactorSourceTypeSecret:
		return enum.RiskClassR3
	case enum.RiskFactorSourceTypeDatabase, enum.RiskFactorSourceTypeRelease, enum.RiskFactorSourceTypeRuntime:
		return enum.RiskClassR2
	case enum.RiskFactorSourceTypePolicy, enum.RiskFactorSourceTypeChangedFile, enum.RiskFactorSourceTypeService, enum.RiskFactorSourceTypeAPI:
		return enum.RiskClassR1
	default:
		return enum.RiskClassR0
	}
}

func riskForTag(tag string) enum.RiskClass {
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "secret", "secrets", "credential", "credentials", "token_scope", "signing_key", "auth", "oidc", "sso":
		return enum.RiskClassR3
	case "production", "prod", "destructive", "delete", "migration", "schema", "backfill", "release", "deploy", "rollback", "runtime":
		return enum.RiskClassR2
	case "api", "service", "worker", "automation":
		return enum.RiskClassR1
	default:
		return enum.RiskClassR0
	}
}

func riskForReviewSignal(signal entity.ReviewSignal) enum.RiskClass {
	if signal.Outcome == enum.ReviewSignalOutcomeBlock || signal.Severity == enum.SignalSeverityCritical {
		return enum.RiskClassR3
	}
	if signal.Outcome == enum.ReviewSignalOutcomeRequestChanges || signal.Outcome == enum.ReviewSignalOutcomeRaiseRisk || signal.Severity == enum.SignalSeverityBlocking {
		return enum.RiskClassR2
	}
	if signal.Severity == enum.SignalSeverityWarning {
		return enum.RiskClassR1
	}
	return enum.RiskClassR0
}

func validRiskFactorSourceType(sourceType enum.RiskFactorSourceType) bool {
	switch sourceType {
	case enum.RiskFactorSourceTypePolicy, enum.RiskFactorSourceTypeChangedFile, enum.RiskFactorSourceTypeService, enum.RiskFactorSourceTypeAPI,
		enum.RiskFactorSourceTypeDatabase, enum.RiskFactorSourceTypeSecret, enum.RiskFactorSourceTypeRelease, enum.RiskFactorSourceTypeRuntime,
		enum.RiskFactorSourceTypeReviewSignal, enum.RiskFactorSourceTypeHumanDecision:
		return true
	default:
		return false
	}
}

func maxRiskClass(left enum.RiskClass, right enum.RiskClass) enum.RiskClass {
	if riskClassRank(right) > riskClassRank(left) {
		return right
	}
	return left
}

func riskClassAtLeast(left enum.RiskClass, right enum.RiskClass) bool {
	return riskClassRank(left) >= riskClassRank(right)
}

func riskClassRank(riskClass enum.RiskClass) int {
	switch riskClass {
	case enum.RiskClassR3:
		return 3
	case enum.RiskClassR2:
		return 2
	case enum.RiskClassR1:
		return 1
	default:
		return 0
	}
}

func ruleReason(rule entity.RiskRule) string {
	for _, text := range rule.ReasonTemplate {
		reason := boundedRiskSummary(text.Text)
		if reason != "" {
			return reason
		}
	}
	return "matched " + string(rule.RuleKind) + " risk rule"
}

func boundedRiskSummary(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || unsafeEvaluationText(value) {
		return ""
	}
	if len(value) > maxEvaluationFactorSummary {
		return value[:maxEvaluationFactorSummary]
	}
	return value
}

func riskExplanation(riskClass enum.RiskClass, factorCount int, requiredGateCount int) string {
	return fmt.Sprintf("risk_class=%s factors=%d required_gates=%d", riskClass, factorCount, requiredGateCount)
}

func matchesString(expected string, actual string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	return expected == strings.TrimSpace(actual)
}

func matchesAnyString(expected string, actuals ...string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	for _, actual := range actuals {
		if expected == strings.TrimSpace(actual) {
			return true
		}
	}
	return false
}

func matchesTag(expected string, tags []string) bool {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return true
	}
	return slices.Contains(tags, expected)
}

func matchesTags(expected []string, tags []string) bool {
	for _, tag := range expected {
		if !matchesTag(tag, tags) {
			return false
		}
	}
	return true
}

func matchesGlob(pattern string, value string) (bool, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true, nil
	}
	matched, err := path.Match(pattern, strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%w: invalid risk rule path glob", errs.ErrInvalidArgument)
	}
	return matched, nil
}

func containsString(value string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), needle)
}

func containsAnyString(needle string, values ...string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func factorRef(factor *value.RiskEvaluationFactor) string {
	if factor == nil {
		return ""
	}
	return factor.Ref
}

func hasFactorRef(factors []entity.RiskFactor, sourceType enum.RiskFactorSourceType, ref string) bool {
	for _, factor := range factors {
		if factor.SourceType == sourceType && factor.SourceRef == ref {
			return true
		}
	}
	return false
}

func riskEvaluationSummaryProvided(summary value.RiskEvaluationSummary) bool {
	return strings.TrimSpace(summary.ChangedFilesSummaryRef) != "" || strings.TrimSpace(summary.Summary) != "" || len(summary.Factors) > 0
}
