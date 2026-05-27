package governance

import (
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

func scanRiskProfile(row postgreslib.RowScanner) (entity.RiskProfile, error) {
	var profile entity.RiskProfile
	var displayName, description []byte
	var status string
	var activeVersion pgtype.Int8
	err := row.Scan(
		&profile.ID,
		&profile.Scope.Type,
		&profile.Scope.Ref,
		&profile.Slug,
		&displayName,
		&description,
		&status,
		&activeVersion,
		&profile.Version,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)
	profile.Status = enum.RiskProfileStatus(status)
	if activeVersion.Valid {
		version := activeVersion.Int64
		profile.ActiveVersion = &version
	}
	if err != nil {
		return profile, err
	}
	if err := unmarshalJSON(displayName, &profile.DisplayName); err != nil {
		return profile, fmt.Errorf("scan risk profile display_name: %w", err)
	}
	if err := unmarshalJSON(description, &profile.Description); err != nil {
		return profile, fmt.Errorf("scan risk profile description: %w", err)
	}
	return profile, nil
}

func scanRiskProfileVersion(row postgreslib.RowScanner) (entity.RiskProfileVersion, error) {
	var version entity.RiskProfileVersion
	var status string
	var activatedAt pgtype.Timestamptz
	err := row.Scan(
		&version.RiskProfileID,
		&version.ProfileVersion,
		&status,
		&version.ContentDigest,
		&version.CreatedAt,
		&activatedAt,
	)
	version.Status = enum.RiskProfileVersionStatus(status)
	version.ActivatedAt = postgreslib.TimePtrFromPG(activatedAt)
	return version, err
}

func scanRiskRule(row postgreslib.RowScanner) (entity.RiskRule, error) {
	var rule entity.RiskRule
	var ruleKind, minRiskClass, status string
	var matcher, reasonTemplate []byte
	var requiredGatePolicyID pgtype.UUID
	err := row.Scan(
		&rule.ID,
		&rule.RiskProfileID,
		&rule.ProfileVersion,
		&ruleKind,
		&matcher,
		&minRiskClass,
		&requiredGatePolicyID,
		&reasonTemplate,
		&status,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)
	rule.RuleKind = enum.RiskRuleKind(ruleKind)
	rule.MinRiskClass = enum.RiskClass(minRiskClass)
	rule.RequiredGatePolicyID = postgreslib.UUIDPtrFromPG(requiredGatePolicyID)
	rule.Status = enum.RuleStatus(status)
	rule.MatcherJSON = append(rule.MatcherJSON[:0], matcher...)
	if err != nil {
		return rule, err
	}
	if err := unmarshalJSON(reasonTemplate, &rule.ReasonTemplate); err != nil {
		return rule, fmt.Errorf("scan risk rule reason_template: %w", err)
	}
	return rule, nil
}

func scanGatePolicy(row postgreslib.RowScanner) (entity.GatePolicy, error) {
	var policy entity.GatePolicy
	var riskProfileID pgtype.UUID
	var gateKind, minRiskClass, status string
	var signalKinds []byte
	err := row.Scan(
		&policy.ID,
		&riskProfileID,
		&policy.ProfileVersion,
		&gateKind,
		&minRiskClass,
		&policy.RequiredActorPolicyRef,
		&signalKinds,
		&policy.TimeoutPolicyRef,
		&status,
	)
	policy.RiskProfileID = postgreslib.UUIDPtrFromPG(riskProfileID)
	policy.GateKind = enum.GateKind(gateKind)
	policy.MinRiskClass = enum.RiskClass(minRiskClass)
	policy.Status = enum.RuleStatus(status)
	if err != nil {
		return policy, err
	}
	if err := unmarshalJSON(signalKinds, &policy.RequiredSignalKinds); err != nil {
		return policy, fmt.Errorf("scan gate policy signal kinds: %w", err)
	}
	return policy, nil
}

func scanRiskAssessment(row postgreslib.RowScanner) (entity.RiskAssessment, error) {
	var assessment entity.RiskAssessment
	var riskProfileID pgtype.UUID
	var riskProfileVersion pgtype.Int8
	var initialRisk, effectiveRisk, status string
	var providerContext, agentContext, runtimeContext, evaluationSummary, evidenceRefs, requiredGates []byte
	err := row.Scan(
		&assessment.ID,
		&assessment.Target.Type,
		&assessment.Target.Ref,
		&assessment.ProjectContext.ProjectRef,
		&assessment.ProjectContext.RepositoryRef,
		&assessment.ProjectContext.ServiceRef,
		&assessment.ProjectContext.BranchRulesRef,
		&assessment.ProjectContext.ReleasePolicyRef,
		&assessment.ProjectContext.ReleaseLineRef,
		&providerContext,
		&agentContext,
		&runtimeContext,
		&riskProfileID,
		&riskProfileVersion,
		&evaluationSummary,
		&evidenceRefs,
		&initialRisk,
		&effectiveRisk,
		&status,
		&assessment.Explanation,
		&requiredGates,
		&assessment.Version,
		&assessment.CreatedAt,
		&assessment.UpdatedAt,
	)
	assessment.ProviderContext = append(assessment.ProviderContext[:0], providerContext...)
	assessment.AgentContext = append(assessment.AgentContext[:0], agentContext...)
	assessment.RuntimeContext = append(assessment.RuntimeContext[:0], runtimeContext...)
	assessment.RiskProfileID = postgreslib.UUIDPtrFromPG(riskProfileID)
	if riskProfileVersion.Valid {
		version := riskProfileVersion.Int64
		assessment.RiskProfileVersion = &version
	}
	assessment.InitialRiskClass = enum.RiskClass(initialRisk)
	assessment.EffectiveRiskClass = enum.RiskClass(effectiveRisk)
	assessment.Status = enum.RiskAssessmentStatus(status)
	if err != nil {
		return assessment, err
	}
	if err := unmarshalJSON(requiredGates, &assessment.RequiredGates); err != nil {
		return assessment, fmt.Errorf("scan risk assessment required gates: %w", err)
	}
	if err := unmarshalJSON(evaluationSummary, &assessment.EvaluationSummary); err != nil {
		return assessment, fmt.Errorf("scan risk assessment evaluation summary: %w", err)
	}
	if err := unmarshalJSON(evidenceRefs, &assessment.EvidenceRefs); err != nil {
		return assessment, fmt.Errorf("scan risk assessment evidence refs: %w", err)
	}
	return assessment, nil
}

func scanRiskFactor(row postgreslib.RowScanner) (entity.RiskFactor, error) {
	var factor entity.RiskFactor
	var sourceType, riskClass string
	err := row.Scan(
		&factor.ID,
		&factor.RiskAssessmentID,
		&sourceType,
		&factor.SourceRef,
		&riskClass,
		&factor.Summary,
		&factor.CreatedAt,
	)
	factor.SourceType = enum.RiskFactorSourceType(sourceType)
	factor.RiskClass = enum.RiskClass(riskClass)
	return factor, err
}

func scanReviewSignal(row postgreslib.RowScanner) (entity.ReviewSignal, error) {
	var signal entity.ReviewSignal
	var riskAssessmentID pgtype.UUID
	var roleKind, outcome, severity, confidence string
	var evidenceRefs []byte
	err := row.Scan(
		&signal.ID,
		&riskAssessmentID,
		&signal.Target.Type,
		&signal.Target.Ref,
		&roleKind,
		&signal.AuthorRef,
		&outcome,
		&severity,
		&confidence,
		&evidenceRefs,
		&signal.Summary,
		&signal.SourceFingerprint,
		&signal.CreatedAt,
	)
	signal.RiskAssessmentID = postgreslib.UUIDPtrFromPG(riskAssessmentID)
	signal.RoleKind = enum.ReviewRoleKind(roleKind)
	signal.Outcome = enum.ReviewSignalOutcome(outcome)
	signal.Severity = enum.SignalSeverity(severity)
	signal.Confidence = enum.Confidence(confidence)
	if err != nil {
		return signal, err
	}
	if err := unmarshalJSON(evidenceRefs, &signal.EvidenceRefs); err != nil {
		return signal, fmt.Errorf("scan review signal evidence refs: %w", err)
	}
	return signal, nil
}

func scanGateRequest(row postgreslib.RowScanner) (entity.GateRequest, error) {
	var request entity.GateRequest
	var riskAssessmentID, gatePolicyID pgtype.UUID
	var interactionDeliveryRef, evidenceRefs []byte
	var terminalAt pgtype.Timestamptz
	var status string
	err := row.Scan(
		&request.ID,
		&riskAssessmentID,
		&gatePolicyID,
		&request.Target.Type,
		&request.Target.Ref,
		&interactionDeliveryRef,
		&evidenceRefs,
		&request.EvidenceSummary,
		&status,
		&request.TerminalActorRef,
		&request.TerminalReason,
		&terminalAt,
		&request.Version,
		&request.CreatedAt,
		&request.UpdatedAt,
	)
	request.RiskAssessmentID = postgreslib.UUIDPtrFromPG(riskAssessmentID)
	request.GatePolicyID = postgreslib.UUIDPtrFromPG(gatePolicyID)
	request.Status = enum.GateRequestStatus(status)
	request.TerminalAt = postgreslib.TimePtrFromPG(terminalAt)
	if err != nil {
		return request, err
	}
	if err := unmarshalJSON(interactionDeliveryRef, &request.InteractionDeliveryRef); err != nil {
		return request, fmt.Errorf("scan gate request interaction ref: %w", err)
	}
	if err := unmarshalJSON(evidenceRefs, &request.EvidenceRefs); err != nil {
		return request, fmt.Errorf("scan gate request evidence refs: %w", err)
	}
	return request, nil
}

func scanGateDecision(row postgreslib.RowScanner) (entity.GateDecision, error) {
	var decision entity.GateDecision
	var outcome string
	dest := []any{
		&decision.ID,
		&decision.GateRequestID,
		&decision.DecisionActorRef,
		&decision.DecisionPolicyRef,
		&outcome,
		&decision.Reason,
		&decision.ConditionsSummary,
		&decision.SourceRef,
		&decision.DecidedAt,
	}
	err := row.Scan(dest...)
	decision.Outcome = enum.GateOutcome(outcome)
	return decision, err
}

func scanReleaseDecisionPackage(row postgreslib.RowScanner) (entity.ReleaseDecisionPackage, error) {
	var item entity.ReleaseDecisionPackage
	var riskAssessmentID pgtype.UUID
	var status string
	var providerRefs, runtimeRefs, agentContext, evidenceRefs, integrationRefs []byte
	err := row.Scan(
		&item.ID,
		&item.ReleaseCandidateRef,
		&item.ProjectContext.ProjectRef,
		&item.ProjectContext.RepositoryRef,
		&item.ProjectContext.ServiceRef,
		&item.ProjectContext.BranchRulesRef,
		&item.ProjectContext.ReleasePolicyRef,
		&item.ProjectContext.ReleaseLineRef,
		&item.RepositoryRefs,
		&riskAssessmentID,
		&providerRefs,
		&runtimeRefs,
		&agentContext,
		&item.ReviewSignalIDs,
		&evidenceRefs,
		&integrationRefs,
		&item.KnownLimitationsSummary,
		&status,
		&item.Version,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.RiskAssessmentID = postgreslib.UUIDPtrFromPG(riskAssessmentID)
	item.ProviderRefs = append(item.ProviderRefs[:0], providerRefs...)
	item.RuntimeRefs = append(item.RuntimeRefs[:0], runtimeRefs...)
	item.AgentContext = append(item.AgentContext[:0], agentContext...)
	item.Status = enum.ReleaseDecisionPackageStatus(status)
	if err != nil {
		return item, err
	}
	if err := unmarshalJSON(evidenceRefs, &item.EvidenceRefs); err != nil {
		return item, fmt.Errorf("scan release package evidence refs: %w", err)
	}
	if err := unmarshalJSON(integrationRefs, &item.IntegrationRefs); err != nil {
		return item, fmt.Errorf("scan release package integration refs: %w", err)
	}
	return item, nil
}

func scanReleaseDecision(row postgreslib.RowScanner) (entity.ReleaseDecision, error) {
	var item entity.ReleaseDecision
	var gateDecisionID pgtype.UUID
	var outcome, status string
	err := row.Scan(
		&item.ID,
		&item.ReleaseDecisionPackageID,
		&gateDecisionID,
		&outcome,
		&item.DecisionActorRef,
		&item.DecisionPolicyRef,
		&item.Reason,
		&item.ConditionsSummary,
		&status,
		&item.Version,
		&item.DecidedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.GateDecisionID = postgreslib.UUIDPtrFromPG(gateDecisionID)
	item.Outcome = enum.ReleaseDecisionOutcome(outcome)
	item.Status = enum.ReleaseDecisionStatus(status)
	return item, err
}

func scanReleaseSafetyState(row postgreslib.RowScanner) (entity.ReleaseSafetyState, error) {
	var item entity.ReleaseSafetyState
	var state string
	err := row.Scan(
		&item.ID,
		&item.ReleaseDecisionPackageID,
		&state,
		&item.RuntimeJobRef,
		&item.BlockingSignalCount,
		&item.LastStateReason,
		&item.Version,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.CurrentState = enum.ReleaseSafetyStateKind(state)
	return item, err
}

func scanBlockingSignal(row postgreslib.RowScanner) (entity.BlockingSignal, error) {
	var item entity.BlockingSignal
	var sourceType, severity, status string
	var resolvedAt pgtype.Timestamptz
	err := row.Scan(
		&item.ID,
		&item.Target.Type,
		&item.Target.Ref,
		&sourceType,
		&item.SourceRef,
		&severity,
		&item.Summary,
		&status,
		&item.Version,
		&item.CreatedAt,
		&item.UpdatedAt,
		&resolvedAt,
	)
	item.SourceType = enum.BlockingSignalSourceType(sourceType)
	item.Severity = enum.SignalSeverity(severity)
	item.Status = enum.BlockingSignalStatus(status)
	item.ResolvedAt = postgreslib.TimePtrFromPG(resolvedAt)
	return item, err
}

func scanCommandResult(row postgreslib.RowScanner) (entity.CommandResult, error) {
	rowResult, err := postgreslib.ScanCommandResultRow(row)
	return entity.CommandResult{
		Key:            rowResult.Key,
		CommandID:      rowResult.CommandID,
		IdempotencyKey: rowResult.IdempotencyKey,
		Actor:          value.Actor{Type: rowResult.ActorType, ID: rowResult.ActorID},
		Operation:      rowResult.Operation,
		AggregateType:  rowResult.AggregateType,
		AggregateID:    rowResult.AggregateID,
		ResultPayload:  rowResult.ResultPayload,
		CreatedAt:      rowResult.CreatedAt,
	}, err
}

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	raw, err := postgreslib.ScanOutboxEventRow(row)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxRowToEntity(raw), nil
}

func outboxRowToEntity(raw postgreslib.OutboxEventRow) entity.OutboxEvent {
	return entity.OutboxEvent{
		Event: outboxlib.NewEvent(
			raw.Identity.RowID,
			raw.Identity.TypeName,
			raw.Identity.ContractVersion,
			raw.Identity.SubjectKind,
			raw.Identity.SubjectID,
			raw.Body,
			raw.Identity.CreatedAt,
			raw.Delivery.Attempts,
		),
		PublishedAt:         raw.Delivery.SentAt,
		NextAttemptAt:       raw.Delivery.RetryAt,
		LockedUntil:         raw.Delivery.LeaseUntil,
		FailedPermanentlyAt: raw.Failure.DeadAt,
		FailureKind:         raw.Failure.FailureCode,
		LastError:           raw.Failure.ErrorText,
	}
}

func unmarshalJSON[T any](payload []byte, out *T) error {
	if len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
}
