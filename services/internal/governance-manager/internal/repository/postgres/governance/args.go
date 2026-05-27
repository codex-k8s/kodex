package governance

import (
	"encoding/json"

	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
)

const (
	defaultPageSize = int32(100)
	maxPageSize     = int32(500)
)

type pageQueryArgs struct {
	pgx.NamedArgs
	PageSize int32
	Offset   int32
}

func riskProfileArgs(profile entity.RiskProfile) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"scope_type":     profile.Scope.Type,
		"scope_ref":      profile.Scope.Ref,
		"slug":           profile.Slug,
		"display_name":   jsonArrayPayload(profile.DisplayName),
		"description":    jsonArrayPayload(profile.Description),
		"status":         string(profile.Status),
		"active_version": nullableInt64(profile.ActiveVersion),
	}, profile.ID, profile.Version, profile.CreatedAt, profile.UpdatedAt)
}

func riskProfileUpdateArgs(profile entity.RiskProfile, previousVersion int64) pgx.NamedArgs {
	args := riskProfileArgs(profile)
	args["previous_version"] = previousVersion
	return args
}

func riskProfileVersionArgs(version entity.RiskProfileVersion) pgx.NamedArgs {
	return pgx.NamedArgs{
		"risk_profile_id": version.RiskProfileID,
		"profile_version": version.ProfileVersion,
		"status":          string(version.Status),
		"content_digest":  version.ContentDigest,
		"created_at":      version.CreatedAt,
		"activated_at":    postgreslib.NullableTime(version.ActivatedAt),
	}
}

func riskRuleArgs(rule entity.RiskRule) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                      rule.ID,
		"risk_profile_id":         rule.RiskProfileID,
		"profile_version":         rule.ProfileVersion,
		"rule_kind":               string(rule.RuleKind),
		"matcher":                 jsonObjectPayload(rule.MatcherJSON),
		"min_risk_class":          string(rule.MinRiskClass),
		"required_gate_policy_id": postgreslib.NullableUUID(rule.RequiredGatePolicyID),
		"reason_template":         jsonArrayPayload(rule.ReasonTemplate),
		"status":                  string(rule.Status),
		"created_at":              rule.CreatedAt,
		"updated_at":              rule.UpdatedAt,
	}
}

func gatePolicyArgs(policy entity.GatePolicy) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                        policy.ID,
		"risk_profile_id":           postgreslib.NullableUUID(policy.RiskProfileID),
		"profile_version":           policy.ProfileVersion,
		"gate_kind":                 string(policy.GateKind),
		"min_risk_class":            string(policy.MinRiskClass),
		"required_actor_policy_ref": policy.RequiredActorPolicyRef,
		"required_signal_kinds":     jsonArrayPayload(policy.RequiredSignalKinds),
		"timeout_policy_ref":        policy.TimeoutPolicyRef,
		"status":                    string(policy.Status),
	}
}

func riskAssessmentArgs(assessment entity.RiskAssessment) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"target_type":          assessment.Target.Type,
		"target_ref":           assessment.Target.Ref,
		"project_ref":          assessment.ProjectContext.ProjectRef,
		"repository_ref":       assessment.ProjectContext.RepositoryRef,
		"service_ref":          assessment.ProjectContext.ServiceRef,
		"branch_rules_ref":     assessment.ProjectContext.BranchRulesRef,
		"release_policy_ref":   assessment.ProjectContext.ReleasePolicyRef,
		"release_line_ref":     assessment.ProjectContext.ReleaseLineRef,
		"provider_context":     jsonObjectPayload(assessment.ProviderContext),
		"agent_context":        jsonObjectPayload(assessment.AgentContext),
		"runtime_context":      jsonObjectPayload(assessment.RuntimeContext),
		"risk_profile_id":      postgreslib.NullableUUID(assessment.RiskProfileID),
		"risk_profile_version": nullableInt64(assessment.RiskProfileVersion),
		"evaluation_summary":   jsonObjectPayload(mustJSON(assessment.EvaluationSummary)),
		"evidence_refs":        jsonArrayPayload(assessment.EvidenceRefs),
		"initial_risk_class":   string(assessment.InitialRiskClass),
		"effective_risk_class": string(assessment.EffectiveRiskClass),
		"status":               string(assessment.Status),
		"explanation":          assessment.Explanation,
		"required_gates":       jsonArrayPayload(assessment.RequiredGates),
	}, assessment.ID, assessment.Version, assessment.CreatedAt, assessment.UpdatedAt)
}

func riskAssessmentUpdateArgs(assessment entity.RiskAssessment, previousVersion int64) pgx.NamedArgs {
	args := riskAssessmentArgs(assessment)
	args["previous_version"] = previousVersion
	return args
}

func riskFactorArgs(factor entity.RiskFactor) pgx.NamedArgs {
	args := pgx.NamedArgs{
		"id":                 factor.ID,
		"risk_assessment_id": factor.RiskAssessmentID,
		"source_type":        string(factor.SourceType),
		"source_ref":         factor.SourceRef,
		"risk_class":         string(factor.RiskClass),
		"summary":            factor.Summary,
	}
	args["created_at"] = factor.CreatedAt
	return args
}

func reviewSignalArgs(signal entity.ReviewSignal) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                 signal.ID,
		"risk_assessment_id": postgreslib.NullableUUID(signal.RiskAssessmentID),
		"target_type":        signal.Target.Type,
		"target_ref":         signal.Target.Ref,
		"role_kind":          string(signal.RoleKind),
		"author_ref":         signal.AuthorRef,
		"outcome":            string(signal.Outcome),
		"severity":           string(signal.Severity),
		"confidence":         string(signal.Confidence),
		"evidence_refs":      jsonArrayPayload(signal.EvidenceRefs),
		"summary":            signal.Summary,
		"source_fingerprint": signal.SourceFingerprint,
		"created_at":         signal.CreatedAt,
	}
}

func gateRequestArgs(request entity.GateRequest) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"risk_assessment_id":       postgreslib.NullableUUID(request.RiskAssessmentID),
		"gate_policy_id":           postgreslib.NullableUUID(request.GatePolicyID),
		"target_type":              request.Target.Type,
		"target_ref":               request.Target.Ref,
		"interaction_delivery_ref": jsonObjectPayload(mustJSON(request.InteractionDeliveryRef)),
		"evidence_refs":            jsonArrayPayload(request.EvidenceRefs),
		"evidence_summary":         request.EvidenceSummary,
		"status":                   string(request.Status),
		"terminal_actor_ref":       request.TerminalActorRef,
		"terminal_reason":          request.TerminalReason,
		"terminal_at":              postgreslib.NullableTime(request.TerminalAt),
	}, request.ID, request.Version, request.CreatedAt, request.UpdatedAt)
}

func gateRequestUpdateArgs(request entity.GateRequest, previousVersion int64) pgx.NamedArgs {
	args := gateRequestArgs(request)
	args["previous_version"] = previousVersion
	return args
}

func gateDecisionArgs(decision entity.GateDecision) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  decision.ID,
		"gate_request_id":     decision.GateRequestID,
		"decision_actor_ref":  decision.DecisionActorRef,
		"decision_policy_ref": decision.DecisionPolicyRef,
		"outcome":             string(decision.Outcome),
		"reason":              decision.Reason,
		"conditions_summary":  decision.ConditionsSummary,
		"source_ref":          decision.SourceRef,
		"decided_at":          decision.DecidedAt,
	}
}

func releaseDecisionPackageArgs(item entity.ReleaseDecisionPackage) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"release_candidate_ref":     item.ReleaseCandidateRef,
		"project_ref":               item.ProjectContext.ProjectRef,
		"repository_ref":            item.ProjectContext.RepositoryRef,
		"service_ref":               item.ProjectContext.ServiceRef,
		"branch_rules_ref":          item.ProjectContext.BranchRulesRef,
		"release_policy_ref":        item.ProjectContext.ReleasePolicyRef,
		"release_line_ref":          item.ProjectContext.ReleaseLineRef,
		"repository_refs":           item.RepositoryRefs,
		"risk_assessment_id":        postgreslib.NullableUUID(item.RiskAssessmentID),
		"provider_refs":             jsonArrayPayloadBytes(item.ProviderRefs),
		"runtime_refs":              jsonArrayPayloadBytes(item.RuntimeRefs),
		"agent_context":             jsonObjectPayload(item.AgentContext),
		"review_signal_ids":         item.ReviewSignalIDs,
		"evidence_refs":             jsonArrayPayload(item.EvidenceRefs),
		"integration_refs":          jsonArrayPayload(item.IntegrationRefs),
		"known_limitations_summary": item.KnownLimitationsSummary,
		"status":                    string(item.Status),
	}, item.ID, item.Version, item.CreatedAt, item.UpdatedAt)
}

func releaseDecisionPackageUpdateArgs(item entity.ReleaseDecisionPackage, previousVersion int64) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":               item.ID,
		"status":           string(item.Status),
		"version":          item.Version,
		"updated_at":       item.UpdatedAt,
		"previous_version": previousVersion,
	}
}

func releaseDecisionArgs(item entity.ReleaseDecision) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"release_decision_package_id": item.ReleaseDecisionPackageID,
		"gate_decision_id":            postgreslib.NullableUUID(item.GateDecisionID),
		"outcome":                     string(item.Outcome),
		"decision_actor_ref":          item.DecisionActorRef,
		"decision_policy_ref":         item.DecisionPolicyRef,
		"reason":                      item.Reason,
		"conditions_summary":          item.ConditionsSummary,
		"status":                      string(item.Status),
		"decided_at":                  item.DecidedAt,
	}, item.ID, item.Version, item.CreatedAt, item.UpdatedAt)
}

func releaseDecisionUpdateArgs(item entity.ReleaseDecision, previousVersion int64) pgx.NamedArgs {
	args := releaseDecisionArgs(item)
	args["previous_version"] = previousVersion
	return args
}

func releaseDecisionFilterArgs(filter query.ReleaseDecisionFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"release_decision_package_id": postgreslib.NullableUUID(filter.ReleaseDecisionPackageID),
		"project_ref":                 filter.ProjectContext.ProjectRef,
		"status":                      string(filter.Status),
		"outcome":                     string(filter.Outcome),
	})
}

func releaseSafetyStateArgs(item entity.ReleaseSafetyState) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"release_decision_package_id": item.ReleaseDecisionPackageID,
		"current_state":               string(item.CurrentState),
		"runtime_job_ref":             item.RuntimeJobRef,
		"blocking_signal_count":       item.BlockingSignalCount,
		"last_state_reason":           item.LastStateReason,
	}, item.ID, item.Version, item.CreatedAt, item.UpdatedAt)
}

func releaseSafetyStateUpdateArgs(item entity.ReleaseSafetyState, previousVersion int64) pgx.NamedArgs {
	args := releaseSafetyStateArgs(item)
	args["previous_version"] = previousVersion
	return args
}

func blockingSignalArgs(item entity.BlockingSignal) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"target_type": item.Target.Type,
		"target_ref":  item.Target.Ref,
		"source_type": string(item.SourceType),
		"source_ref":  item.SourceRef,
		"severity":    string(item.Severity),
		"summary":     item.Summary,
		"status":      string(item.Status),
		"resolved_at": postgreslib.NullableTime(item.ResolvedAt),
	}, item.ID, item.Version, item.CreatedAt, item.UpdatedAt)
}

func blockingSignalUpdateArgs(item entity.BlockingSignal, previousVersion int64) pgx.NamedArgs {
	args := blockingSignalArgs(item)
	args["previous_version"] = previousVersion
	return args
}

func commandResultArgs(result entity.CommandResult) pgx.NamedArgs {
	return pgx.NamedArgs{
		"key":             result.Key,
		"command_id":      postgreslib.NullableUUID(result.CommandID),
		"idempotency_key": result.IdempotencyKey,
		"actor_type":      result.Actor.Type,
		"actor_id":        result.Actor.ID,
		"operation":       result.Operation,
		"aggregate_type":  result.AggregateType,
		"aggregate_id":    result.AggregateID,
		"result_payload":  jsonObjectPayload(result.ResultPayload),
		"created_at":      result.CreatedAt,
	}
}

func commandIdentityArgs(identity query.CommandIdentity) pgx.NamedArgs {
	return pgx.NamedArgs{
		"command_id":      postgreslib.NullableUUID(identity.CommandID),
		"idempotency_key": identity.IdempotencyKey,
		"operation":       identity.Operation,
		"actor_type":      identity.Actor.Type,
		"actor_id":        identity.Actor.ID,
	}
}

func outboxEventArgs(event entity.OutboxEvent) pgx.NamedArgs {
	return postgreslib.OutboxCreateArgs(
		event.ID,
		event.EventType,
		event.SchemaVersion,
		event.AggregateType,
		event.AggregateID,
		event.Payload,
		event.OccurredAt,
		event.PublishedAt,
	)
}

func riskProfileFilterArgs(filter query.RiskProfileFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"scope_type": filter.Scope.Type,
		"scope_ref":  filter.Scope.Ref,
		"status":     string(filter.Status),
	})
}

func ruleFilterArgs(filter query.RuleFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"risk_profile_id": filter.RiskProfileID,
		"profile_version": filter.ProfileVersion,
		"rule_kind":       string(filter.RuleKind),
		"status":          string(filter.Status),
	})
}

func gatePolicyFilterArgs(filter query.GatePolicyFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"risk_profile_id": filter.RiskProfileID,
		"profile_version": filter.ProfileVersion,
		"gate_kind":       string(filter.GateKind),
		"status":          string(filter.Status),
	})
}

func riskAssessmentFilterArgs(filter query.RiskAssessmentFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"target_type":          filter.Target.Type,
		"target_ref":           filter.Target.Ref,
		"project_ref":          filter.ProjectContext.ProjectRef,
		"repository_ref":       filter.ProjectContext.RepositoryRef,
		"effective_risk_class": string(filter.EffectiveRiskClass),
		"status":               string(filter.Status),
	})
}

func riskFactorFilterArgs(filter query.RiskFactorFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"risk_assessment_id": filter.RiskAssessmentID,
		"source_type":        string(filter.SourceType),
	})
}

func reviewSignalFilterArgs(filter query.ReviewSignalFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"risk_assessment_id": postgreslib.NullableUUID(filter.RiskAssessmentID),
		"target_type":        filter.Target.Type,
		"target_ref":         filter.Target.Ref,
		"role_kind":          string(filter.RoleKind),
		"outcome":            string(filter.Outcome),
	})
}

func gateRequestFilterArgs(filter query.GateRequestFilter) pageQueryArgs {
	return withGateTargetPage(filter.Page, filter.Target.Type, filter.Target.Ref, pgx.NamedArgs{
		"risk_assessment_id": postgreslib.NullableUUID(filter.RiskAssessmentID),
		"status":             string(filter.Status),
	})
}

func gateDecisionFilterArgs(filter query.GateDecisionFilter) pageQueryArgs {
	return withGateTargetPage(filter.Page, filter.Target.Type, filter.Target.Ref, pgx.NamedArgs{
		"gate_request_id": postgreslib.NullableUUID(filter.GateRequestID),
		"outcome":         string(filter.Outcome),
	})
}

func releaseDecisionPackageFilterArgs(filter query.ReleaseDecisionPackageFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"project_ref":           filter.ProjectContext.ProjectRef,
		"release_candidate_ref": filter.ReleaseCandidateRef,
		"status":                string(filter.Status),
	})
}

func blockingSignalFilterArgs(filter query.BlockingSignalFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"target_type": filter.Target.Type,
		"target_ref":  filter.Target.Ref,
		"status":      string(filter.Status),
		"severity":    string(filter.Severity),
	})
}

func withPage(page query.PageRequest, args pgx.NamedArgs) pageQueryArgs {
	limit, offset, _ := postgreslib.AddOffsetPageArgs(args, page.PageSize, page.PageToken, defaultPageSize, maxPageSize)
	return pageQueryArgs{NamedArgs: args, PageSize: limit, Offset: offset}
}

func withGateTargetPage(page query.PageRequest, targetType string, targetRef string, args pgx.NamedArgs) pageQueryArgs {
	args["target_type"] = targetType
	args["target_ref"] = targetRef
	return withPage(page, args)
}

func pageResult[T any](items []T, page pageQueryArgs) ([]T, query.PageResult) {
	trimmed, token := postgreslib.TrimOffsetPage(items, page.PageSize, page.Offset+page.PageSize)
	return trimmed, query.PageResult{NextPageToken: token}
}

func jsonArrayPayload(value any) string {
	payload := mustJSON(value)
	if len(payload) == 0 || string(payload) == "null" {
		return "[]"
	}
	return string(payload)
}

func jsonArrayPayloadBytes(payload []byte) string {
	if len(payload) == 0 || string(payload) == "null" {
		return "[]"
	}
	return string(payload)
}

func jsonObjectPayload(payload []byte) string {
	if len(payload) == 0 || string(payload) == "null" {
		return "{}"
	}
	return string(payload)
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func mustJSON(value any) []byte {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return payload
}
