package agent

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func scanFlow(row postgreslib.RowScanner) (entity.Flow, error) {
	var flow entity.Flow
	var displayName, description []byte
	var status string
	var activeVersionID pgtype.UUID
	err := row.Scan(
		&flow.ID,
		&flow.Scope.Type,
		&flow.Scope.Ref,
		&flow.Slug,
		&displayName,
		&description,
		&flow.IconObjectURI,
		&status,
		&activeVersionID,
		&flow.Version,
		&flow.CreatedAt,
		&flow.UpdatedAt,
	)
	flow.Status = enum.FlowStatus(status)
	flow.ActiveVersionID = postgreslib.UUIDPtrFromPG(activeVersionID)
	if err != nil {
		return flow, err
	}
	flow.DisplayName, err = localizedTextFromPayload(displayName)
	if err != nil {
		return flow, fmt.Errorf("scan flow display_name: %w", err)
	}
	flow.Description, err = localizedTextFromPayload(description)
	if err != nil {
		return flow, fmt.Errorf("scan flow description: %w", err)
	}
	return flow, nil
}

func scanFlowVersion(row postgreslib.RowScanner) (entity.FlowVersion, error) {
	var version entity.FlowVersion
	var status string
	var activatedAt pgtype.Timestamptz
	err := row.Scan(
		&version.ID,
		&version.FlowID,
		&version.Version,
		&version.SourceRef,
		&version.DefinitionDigest,
		&status,
		&activatedAt,
		&version.CreatedAt,
	)
	version.Status = enum.FlowVersionStatus(status)
	version.ActivatedAt = postgreslib.TimePtrFromPG(activatedAt)
	return version, err
}

func scanStage(row postgreslib.RowScanner) (entity.Stage, error) {
	var stage entity.Stage
	var displayName, requiredArtifacts, acceptancePolicy []byte
	var stageType string
	err := row.Scan(
		&stage.ID,
		&stage.FlowVersionID,
		&stage.Slug,
		&stageType,
		&displayName,
		&stage.IconObjectURI,
		&requiredArtifacts,
		&acceptancePolicy,
		&stage.Position,
	)
	stage.StageType = enum.StageType(stageType)
	stage.RequiredArtifactsJSON = append(stage.RequiredArtifactsJSON[:0], requiredArtifacts...)
	stage.AcceptancePolicyJSON = append(stage.AcceptancePolicyJSON[:0], acceptancePolicy...)
	if err != nil {
		return stage, err
	}
	stage.DisplayName, err = localizedTextFromPayload(displayName)
	if err != nil {
		return stage, fmt.Errorf("scan stage display_name: %w", err)
	}
	return stage, nil
}

func scanStageTransition(row postgreslib.RowScanner) (entity.StageTransition, error) {
	var transition entity.StageTransition
	var fromStageID pgtype.UUID
	var condition []byte
	err := row.Scan(
		&transition.ID,
		&transition.FlowVersionID,
		&fromStageID,
		&transition.ToStageID,
		&condition,
		&transition.FollowUpType,
		&transition.Position,
	)
	transition.FromStageID = postgreslib.UUIDPtrFromPG(fromStageID)
	transition.ConditionJSON = append(transition.ConditionJSON[:0], condition...)
	return transition, err
}

func scanStageRoleBinding(row postgreslib.RowScanner) (entity.StageRoleBinding, error) {
	var binding entity.StageRoleBinding
	var bindingKind string
	var launchPolicy []byte
	err := row.Scan(
		&binding.ID,
		&binding.StageID,
		&binding.RoleProfileID,
		&bindingKind,
		&launchPolicy,
		&binding.RequiredForAcceptance,
	)
	binding.BindingKind = enum.StageRoleBindingKind(bindingKind)
	binding.LaunchPolicyJSON = append(binding.LaunchPolicyJSON[:0], launchPolicy...)
	return binding, err
}

func scanRoleProfile(row postgreslib.RowScanner) (entity.RoleProfile, error) {
	var role entity.RoleProfile
	var displayName, allowedTools []byte
	var roleKind, status string
	err := row.Scan(
		&role.ID,
		&role.Scope.Type,
		&role.Scope.Ref,
		&role.Slug,
		&displayName,
		&role.IconObjectURI,
		&roleKind,
		&role.RuntimeProfile,
		&allowedTools,
		&role.ProviderAccountPolicyRef,
		&status,
		&role.Version,
		&role.CreatedAt,
		&role.UpdatedAt,
	)
	role.RoleKind = enum.RoleKind(roleKind)
	role.Status = enum.RoleStatus(status)
	if err != nil {
		return role, err
	}
	role.DisplayName, err = localizedTextFromPayload(displayName)
	if err != nil {
		return role, fmt.Errorf("scan role display_name: %w", err)
	}
	role.AllowedMCPTools, err = stringSliceFromPayload(allowedTools)
	if err != nil {
		return role, fmt.Errorf("scan role allowed_mcp_tools: %w", err)
	}
	return role, nil
}

func scanPromptTemplate(row postgreslib.RowScanner) (entity.PromptTemplate, error) {
	var template entity.PromptTemplate
	var promptKind string
	var activeVersionID pgtype.UUID
	err := row.Scan(
		&template.ID,
		&template.RoleProfileID,
		&promptKind,
		&activeVersionID,
		&template.Version,
		&template.CreatedAt,
		&template.UpdatedAt,
	)
	template.PromptKind = enum.PromptKind(promptKind)
	template.ActiveVersionID = postgreslib.UUIDPtrFromPG(activeVersionID)
	return template, err
}

func scanPromptTemplateVersion(row postgreslib.RowScanner) (entity.PromptTemplateVersion, error) {
	var version entity.PromptTemplateVersion
	var promptKind, status string
	var objectSize pgtype.Int8
	var activatedAt pgtype.Timestamptz
	err := row.Scan(
		&version.ID,
		&version.PromptTemplateID,
		&version.RoleProfileID,
		&promptKind,
		&version.Version,
		&version.SourceRef,
		&version.TemplateObject.ObjectURI,
		&version.TemplateObject.ObjectDigest,
		&objectSize,
		&version.TemplateDigest,
		&status,
		&activatedAt,
		&version.CreatedAt,
	)
	version.PromptKind = enum.PromptKind(promptKind)
	version.Status = enum.PromptVersionStatus(status)
	version.ActivatedAt = postgreslib.TimePtrFromPG(activatedAt)
	if objectSize.Valid {
		size := objectSize.Int64
		version.TemplateObject.ObjectSizeBytes = &size
	}
	return version, err
}

func scanAgentSession(row postgreslib.RowScanner) (entity.AgentSession, error) {
	var session entity.AgentSession
	var flowVersionID, currentStageID, latestSnapshotID pgtype.UUID
	var status string
	err := row.Scan(
		&session.ID,
		&session.Scope.Type,
		&session.Scope.Ref,
		&session.ProviderWorkItemRef,
		&flowVersionID,
		&currentStageID,
		&latestSnapshotID,
		&status,
		&session.CreatedByActorRef,
		&session.Version,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	session.FlowVersionID = postgreslib.UUIDPtrFromPG(flowVersionID)
	session.CurrentStageID = postgreslib.UUIDPtrFromPG(currentStageID)
	session.LatestStateSnapshotID = postgreslib.UUIDPtrFromPG(latestSnapshotID)
	session.Status = enum.AgentSessionStatus(status)
	return session, err
}

func scanAgentRun(row postgreslib.RowScanner) (entity.AgentRun, error) {
	var run entity.AgentRun
	if err := scanAgentRunWithExtra(row, &run); err != nil {
		return run, err
	}
	return run, nil
}

func scanAgentRunListItem(row postgreslib.RowScanner) (entity.AgentRunListItem, error) {
	var item entity.AgentRunListItem
	var gateRef, followUpRef pgtype.UUID
	var gateReason pgtype.Text
	var sortBucket pgtype.Int4
	var sortTime pgtype.Timestamptz
	var latestActivity agentActivitySummaryRow
	err := scanAgentRunWithExtra(row, &item.Run,
		&gateRef,
		&gateReason,
		&followUpRef,
		&latestActivity.id,
		&latestActivity.kind,
		&latestActivity.status,
		&latestActivity.toolName,
		&latestActivity.toolCategory,
		&latestActivity.safeSummary,
		&latestActivity.payloadDigest,
		&latestActivity.boundedError,
		&latestActivity.startedAt,
		&latestActivity.finishedAt,
		&latestActivity.version,
		&latestActivity.updatedAt,
		&sortBucket,
		&sortTime,
	)
	if err != nil {
		return item, err
	}
	item.HumanGateRequestRef = uuidTextFromPG(gateRef)
	item.HumanGateReasonCode = textFromPG(gateReason)
	item.HumanGateWaiting = item.HumanGateRequestRef != ""
	item.FollowUpWaiting = uuidTextFromPG(followUpRef) != ""
	item.LatestActivity = latestActivity.toEntity()
	if sortBucket.Valid {
		item.SortBucket = sortBucket.Int32
	}
	item.SortTime = timeFromPG(sortTime)
	return item, nil
}

func scanAgentRunWithExtra(row postgreslib.RowScanner, run *entity.AgentRun, extra ...any) error {
	var flowVersionID, stageID pgtype.UUID
	var runtimeContext, providerTarget, guidanceRefs []byte
	var status string
	var startedAt, finishedAt pgtype.Timestamptz
	base := []any{
		&run.ID,
		&run.SessionID,
		&flowVersionID,
		&stageID,
		&run.RoleProfileID,
		&run.RoleProfileVersion,
		&run.RoleProfileDigest,
		&run.PromptTemplateVersionID,
		&run.PromptTemplateDigest,
		&runtimeContext,
		&providerTarget,
		&guidanceRefs,
		&status,
		&run.ResultSummary,
		&run.FailureCode,
		&run.Version,
		&startedAt,
		&finishedAt,
		&run.CreatedAt,
		&run.UpdatedAt,
	}
	if err := row.Scan(append(base, extra...)...); err != nil {
		return err
	}
	run.FlowVersionID = postgreslib.UUIDPtrFromPG(flowVersionID)
	run.StageID = postgreslib.UUIDPtrFromPG(stageID)
	run.Status = enum.AgentRunStatus(status)
	run.StartedAt = postgreslib.TimePtrFromPG(startedAt)
	run.FinishedAt = postgreslib.TimePtrFromPG(finishedAt)
	if err := json.Unmarshal(runtimeContext, &run.RuntimeContext); err != nil {
		return fmt.Errorf("scan run runtime_context: %w", err)
	}
	if err := json.Unmarshal(providerTarget, &run.ProviderTarget); err != nil {
		return fmt.Errorf("scan run provider_target: %w", err)
	}
	if err := json.Unmarshal(guidanceRefs, &run.GuidanceRefs); err != nil {
		return fmt.Errorf("scan run guidance_refs: %w", err)
	}
	return nil
}

func scanAgentSessionListItem(row postgreslib.RowScanner) (entity.AgentSessionListItem, error) {
	var item entity.AgentSessionListItem
	var flowVersionID, currentStageID, latestSnapshotID, latestRunID pgtype.UUID
	var status string
	var latestRunStatus pgtype.Text
	var latestRunRuntimeContext []byte
	var latestRunSummary, gateReason pgtype.Text
	var gateRef, followUpRef pgtype.UUID
	var activeRunCount pgtype.Int4
	var sortBucket pgtype.Int4
	var sortTime pgtype.Timestamptz
	var latestActivity agentActivitySummaryRow
	err := row.Scan(
		&item.Session.ID,
		&item.Session.Scope.Type,
		&item.Session.Scope.Ref,
		&item.Session.ProviderWorkItemRef,
		&flowVersionID,
		&currentStageID,
		&latestSnapshotID,
		&status,
		&item.Session.CreatedByActorRef,
		&item.Session.Version,
		&item.Session.CreatedAt,
		&item.Session.UpdatedAt,
		&latestRunID,
		&latestRunStatus,
		&latestRunRuntimeContext,
		&latestRunSummary,
		&activeRunCount,
		&gateRef,
		&gateReason,
		&followUpRef,
		&latestActivity.id,
		&latestActivity.kind,
		&latestActivity.status,
		&latestActivity.toolName,
		&latestActivity.toolCategory,
		&latestActivity.safeSummary,
		&latestActivity.payloadDigest,
		&latestActivity.boundedError,
		&latestActivity.startedAt,
		&latestActivity.finishedAt,
		&latestActivity.version,
		&latestActivity.updatedAt,
		&sortBucket,
		&sortTime,
	)
	if err != nil {
		return item, err
	}
	item.Session.FlowVersionID = postgreslib.UUIDPtrFromPG(flowVersionID)
	item.Session.CurrentStageID = postgreslib.UUIDPtrFromPG(currentStageID)
	item.Session.LatestStateSnapshotID = postgreslib.UUIDPtrFromPG(latestSnapshotID)
	item.Session.Status = enum.AgentSessionStatus(status)
	item.LatestRunID = postgreslib.UUIDPtrFromPG(latestRunID)
	item.LatestRunStatus = enum.AgentRunStatus(textFromPG(latestRunStatus))
	item.LatestRunSafeSummary = textFromPG(latestRunSummary)
	item.LatestRuntimeJobRef = runtimeJobRefFromPayload(latestRunRuntimeContext)
	if activeRunCount.Valid {
		item.ActiveRunCount = activeRunCount.Int32
	}
	item.HumanGateRequestRef = uuidTextFromPG(gateRef)
	item.HumanGateReasonCode = textFromPG(gateReason)
	item.HumanGateWaiting = item.HumanGateRequestRef != ""
	item.FollowUpRef = uuidTextFromPG(followUpRef)
	item.FollowUpWaiting = item.FollowUpRef != ""
	item.LatestActivity = latestActivity.toEntity()
	if sortBucket.Valid {
		item.SortBucket = sortBucket.Int32
	}
	item.SortTime = timeFromPG(sortTime)
	return item, nil
}

func scanSessionStateSnapshot(row postgreslib.RowScanner) (entity.AgentSessionStateSnapshot, error) {
	var snapshot entity.AgentSessionStateSnapshot
	var runID pgtype.UUID
	var snapshotKind string
	var turnIndex, objectSize pgtype.Int8
	err := row.Scan(
		&snapshot.ID,
		&snapshot.SessionID,
		&runID,
		&snapshotKind,
		&turnIndex,
		&snapshot.Object.ObjectURI,
		&snapshot.Object.ObjectDigest,
		&objectSize,
		&snapshot.CapturedAt,
		&snapshot.CreatedAt,
	)
	snapshot.RunID = postgreslib.UUIDPtrFromPG(runID)
	snapshot.SnapshotKind = enum.AgentSessionSnapshotKind(snapshotKind)
	if turnIndex.Valid {
		value := turnIndex.Int64
		snapshot.TurnIndex = &value
	}
	if objectSize.Valid {
		value := objectSize.Int64
		snapshot.Object.ObjectSizeBytes = &value
	}
	return snapshot, err
}

func scanAcceptanceResult(row postgreslib.RowScanner) (entity.AcceptanceResult, error) {
	var acceptance entity.AcceptanceResult
	var runID, stageID pgtype.UUID
	var checkKind, status string
	var details []byte
	err := row.Scan(
		&acceptance.ID,
		&acceptance.SessionID,
		&runID,
		&stageID,
		&checkKind,
		&status,
		&acceptance.TargetRef,
		&details,
		&acceptance.GovernanceContext.RiskAssessmentRef,
		&acceptance.GovernanceContext.GateRequestRef,
		&acceptance.GovernanceContext.GateDecisionRef,
		&acceptance.GovernanceContext.ReleaseDecisionPackageRef,
		&acceptance.GovernanceContext.ReleaseDecisionRef,
		&acceptance.GovernanceContext.RiskProfileRef,
		&acceptance.GovernanceContext.GatePolicyRef,
		&acceptance.GovernanceContext.ReleasePolicyRef,
		&acceptance.Version,
		&acceptance.CreatedAt,
		&acceptance.UpdatedAt,
	)
	acceptance.RunID = postgreslib.UUIDPtrFromPG(runID)
	acceptance.StageID = postgreslib.UUIDPtrFromPG(stageID)
	acceptance.CheckKind = enum.AcceptanceCheckKind(checkKind)
	acceptance.Status = enum.AcceptanceStatus(status)
	acceptance.DetailsJSON = append(acceptance.DetailsJSON[:0], details...)
	return acceptance, err
}

func scanFollowUpIntent(row postgreslib.RowScanner) (entity.FollowUpIntent, error) {
	var intent entity.FollowUpIntent
	var runID, fromStageID, toStageID, acceptanceResultID pgtype.UUID
	var status string
	err := row.Scan(
		&intent.ID,
		&intent.SessionID,
		&runID,
		&fromStageID,
		&toStageID,
		&acceptanceResultID,
		&intent.ProviderTarget.WorkItemRef,
		&intent.ProviderTarget.PullRequestRef,
		&intent.ProviderTarget.CommentRef,
		&intent.ProviderTarget.ReviewSignalRef,
		&intent.ProviderWorkItemType,
		&intent.ProviderOperationRef,
		&intent.InstructionBodyDigest,
		&intent.SafeTitle,
		&intent.SafeSummary,
		&intent.RoleHint,
		&intent.StageHint,
		&intent.IdempotencyKey,
		&intent.GovernanceContext.RiskAssessmentRef,
		&intent.GovernanceContext.GateRequestRef,
		&intent.GovernanceContext.GateDecisionRef,
		&intent.GovernanceContext.ReleaseDecisionPackageRef,
		&intent.GovernanceContext.ReleaseDecisionRef,
		&intent.GovernanceContext.RiskProfileRef,
		&intent.GovernanceContext.GatePolicyRef,
		&intent.GovernanceContext.ReleasePolicyRef,
		&status,
		&intent.Version,
		&intent.CreatedAt,
		&intent.UpdatedAt,
	)
	intent.RunID = postgreslib.UUIDPtrFromPG(runID)
	intent.FromStageID = postgreslib.UUIDPtrFromPG(fromStageID)
	intent.ToStageID = postgreslib.UUIDPtrFromPG(toStageID)
	intent.AcceptanceResultID = postgreslib.UUIDPtrFromPG(acceptanceResultID)
	intent.Status = enum.FollowUpIntentStatus(status)
	return intent, err
}

func scanAgentActivity(row postgreslib.RowScanner) (entity.AgentActivity, error) {
	var activity entity.AgentActivity
	var runID pgtype.UUID
	var activityKind, status string
	var finishedAt pgtype.Timestamptz
	var durationMs pgtype.Int8
	var refs, details []byte
	err := row.Scan(
		&activity.ID,
		&activity.SessionID,
		&runID,
		&activity.TurnID,
		&activity.ToolUseID,
		&activityKind,
		&activity.ToolName,
		&activity.ToolCategory,
		&status,
		&activity.StartedAt,
		&finishedAt,
		&durationMs,
		&activity.SafeSummary,
		&activity.PayloadDigest,
		&activity.BoundedError,
		&refs,
		&details,
		&activity.CorrelationID,
		&activity.IdempotencyKey,
		&activity.Version,
		&activity.CreatedAt,
		&activity.UpdatedAt,
	)
	activity.RunID = postgreslib.UUIDPtrFromPG(runID)
	activity.ActivityKind = enum.AgentActivityKind(activityKind)
	activity.Status = enum.AgentActivityStatus(status)
	activity.FinishedAt = postgreslib.TimePtrFromPG(finishedAt)
	if durationMs.Valid {
		value := durationMs.Int64
		activity.DurationMs = &value
	}
	activity.SafeRefsJSON = append(activity.SafeRefsJSON[:0], refs...)
	activity.SafeDetailsJSON = append(activity.SafeDetailsJSON[:0], details...)
	return activity, err
}

func scanHumanGateRequest(row postgreslib.RowScanner) (entity.HumanGateRequest, error) {
	var gate entity.HumanGateRequest
	var runID, stageID, acceptanceResultID pgtype.UUID
	var resolvedAt pgtype.Timestamptz
	var status, outcome string
	err := row.Scan(
		&gate.ID,
		&gate.SessionID,
		&runID,
		&stageID,
		&acceptanceResultID,
		&gate.ProviderTarget.WorkItemRef,
		&gate.ProviderTarget.PullRequestRef,
		&gate.ProviderTarget.CommentRef,
		&gate.ProviderTarget.ReviewSignalRef,
		&gate.TargetRef,
		&gate.RequestKind,
		&gate.ReasonCode,
		&gate.SafeSummary,
		&gate.InteractionRequestRef,
		&gate.InteractionResponseRef,
		&gate.GovernanceGateRequestRef,
		&gate.GovernanceDecisionRef,
		&gate.GovernanceContext.RiskAssessmentRef,
		&gate.GovernanceContext.ReleaseDecisionPackageRef,
		&gate.GovernanceContext.ReleaseDecisionRef,
		&gate.GovernanceContext.RiskProfileRef,
		&gate.GovernanceContext.GatePolicyRef,
		&gate.GovernanceContext.ReleasePolicyRef,
		&gate.IdempotencyKey,
		&status,
		&outcome,
		&gate.Version,
		&resolvedAt,
		&gate.CreatedAt,
		&gate.UpdatedAt,
	)
	gate.RunID = postgreslib.UUIDPtrFromPG(runID)
	gate.StageID = postgreslib.UUIDPtrFromPG(stageID)
	gate.AcceptanceResultID = postgreslib.UUIDPtrFromPG(acceptanceResultID)
	gate.Status = enum.HumanGateStatus(status)
	gate.Outcome = enum.HumanGateOutcome(outcome)
	gate.ResolvedAt = postgreslib.TimePtrFromPG(resolvedAt)
	gate.GovernanceContext.GateRequestRef = gate.GovernanceGateRequestRef
	gate.GovernanceContext.GateDecisionRef = gate.GovernanceDecisionRef
	return gate, err
}

func scanSelfDeployPlan(row postgreslib.RowScanner) (entity.SelfDeployPlan, error) {
	var plan entity.SelfDeployPlan
	var affectedServiceKeys, pathCategories, expectedRuntimeJobTypes, runtimeBuildJobs []byte
	var status, runtimeBuildStatus string
	err := row.Scan(
		&plan.ID,
		&plan.Scope.Type,
		&plan.Scope.Ref,
		&plan.ProjectRef,
		&plan.RepositoryRef,
		&plan.ProviderSignalRef,
		&plan.SourceRef,
		&plan.MergeCommitSHA,
		&plan.ServicesYAMLRef,
		&plan.ServicesYAMLDigest,
		&affectedServiceKeys,
		&pathCategories,
		&expectedRuntimeJobTypes,
		&plan.GovernanceContext.RiskAssessmentRef,
		&plan.GovernanceContext.GateRequestRef,
		&plan.GovernanceContext.GateDecisionRef,
		&plan.GovernanceContext.ReleaseDecisionPackageRef,
		&plan.GovernanceContext.ReleaseDecisionRef,
		&plan.GovernanceContext.RiskProfileRef,
		&plan.GovernanceContext.GatePolicyRef,
		&plan.GovernanceContext.ReleasePolicyRef,
		&plan.SafeSummary,
		&plan.PlanFingerprint,
		&plan.IdempotencyKey,
		&status,
		&runtimeBuildJobs,
		&runtimeBuildStatus,
		&plan.RuntimeBuildFingerprint,
		&plan.RuntimeBuildErrorCode,
		&plan.RuntimeBuildSummary,
		&plan.Version,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	)
	plan.Status = enum.SelfDeployPlanStatus(status)
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatus(runtimeBuildStatus)
	if err != nil {
		return plan, err
	}
	if err := json.Unmarshal(affectedServiceKeys, &plan.AffectedServiceKeys); err != nil {
		return plan, fmt.Errorf("scan self deploy affected_service_keys: %w", err)
	}
	categories, err := stringSliceFromPayload(pathCategories)
	if err != nil {
		return plan, fmt.Errorf("scan self deploy path_categories: %w", err)
	}
	plan.PathCategories = make([]enum.SelfDeployPathCategory, 0, len(categories))
	for _, category := range categories {
		plan.PathCategories = append(plan.PathCategories, enum.SelfDeployPathCategory(category))
	}
	jobTypes, err := stringSliceFromPayload(expectedRuntimeJobTypes)
	if err != nil {
		return plan, fmt.Errorf("scan self deploy expected_runtime_job_types: %w", err)
	}
	plan.ExpectedRuntimeJobTypes = make([]enum.SelfDeployRuntimeJobType, 0, len(jobTypes))
	for _, jobType := range jobTypes {
		plan.ExpectedRuntimeJobTypes = append(plan.ExpectedRuntimeJobTypes, enum.SelfDeployRuntimeJobType(jobType))
	}
	if err := json.Unmarshal(runtimeBuildJobs, &plan.RuntimeBuildJobs); err != nil {
		return plan, fmt.Errorf("scan self deploy runtime_build_jobs: %w", err)
	}
	return plan, nil
}

func scanCommandResult(row postgreslib.RowScanner) (entity.CommandResult, error) {
	var raw commandResultRow
	if err := raw.scan(row); err != nil {
		return entity.CommandResult{}, err
	}
	return raw.toEntity(), nil
}

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	raw, err := postgreslib.ScanOutboxEventRow(row)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxRowToEntity(raw), nil
}

type commandResultRow struct {
	result        entity.CommandResult
	commandID     pgtype.UUID
	aggregateType string
	payload       []byte
}

func (r *commandResultRow) scan(row postgreslib.RowScanner) error {
	return row.Scan(
		&r.result.Key,
		&r.commandID,
		&r.result.IdempotencyKey,
		&r.result.Actor.Type,
		&r.result.Actor.ID,
		&r.result.Operation,
		&r.aggregateType,
		&r.result.AggregateID,
		&r.payload,
		&r.result.CreatedAt,
	)
}

func (r commandResultRow) toEntity() entity.CommandResult {
	result := r.result
	result.CommandID = postgreslib.UUIDPtrFromPG(r.commandID)
	result.AggregateType = enum.CommandAggregateType(r.aggregateType)
	result.ResultPayload = append(result.ResultPayload[:0], r.payload...)
	return result
}

type agentActivitySummaryRow struct {
	id            pgtype.UUID
	kind          pgtype.Text
	status        pgtype.Text
	toolName      pgtype.Text
	toolCategory  pgtype.Text
	safeSummary   pgtype.Text
	payloadDigest pgtype.Text
	boundedError  pgtype.Text
	startedAt     pgtype.Timestamptz
	finishedAt    pgtype.Timestamptz
	version       pgtype.Int8
	updatedAt     pgtype.Timestamptz
}

func (r agentActivitySummaryRow) toEntity() *entity.AgentActivitySummary {
	id := postgreslib.UUIDPtrFromPG(r.id)
	if id == nil {
		return nil
	}
	summary := &entity.AgentActivitySummary{
		ID:            *id,
		ActivityKind:  enum.AgentActivityKind(textFromPG(r.kind)),
		Status:        enum.AgentActivityStatus(textFromPG(r.status)),
		ToolName:      textFromPG(r.toolName),
		ToolCategory:  textFromPG(r.toolCategory),
		SafeSummary:   textFromPG(r.safeSummary),
		PayloadDigest: textFromPG(r.payloadDigest),
		BoundedError:  textFromPG(r.boundedError),
		StartedAt:     postgreslib.TimePtrFromPG(r.startedAt),
		FinishedAt:    postgreslib.TimePtrFromPG(r.finishedAt),
		UpdatedAt:     timeFromPG(r.updatedAt),
	}
	if r.version.Valid {
		summary.Version = r.version.Int64
	}
	return summary
}

func textFromPG(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func uuidTextFromPG(value pgtype.UUID) string {
	id := postgreslib.UUIDPtrFromPG(value)
	if id == nil {
		return ""
	}
	return id.String()
}

func timeFromPG(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

func runtimeJobRefFromPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var runtimeContext value.RuntimeContextRef
	if err := json.Unmarshal(payload, &runtimeContext); err != nil {
		return ""
	}
	return runtimeContext.JobRef
}

func outboxRowToEntity(raw postgreslib.OutboxEventRow) entity.OutboxEvent {
	event := outboxlib.NewEvent(
		raw.Identity.RowID,
		raw.Identity.TypeName,
		raw.Identity.ContractVersion,
		raw.Identity.SubjectKind,
		raw.Identity.SubjectID,
		raw.Body,
		raw.Identity.CreatedAt,
		raw.Delivery.Attempts,
	)
	return entity.OutboxEvent{
		Event:               event,
		PublishedAt:         raw.Delivery.SentAt,
		NextAttemptAt:       raw.Delivery.RetryAt,
		LockedUntil:         raw.Delivery.LeaseUntil,
		FailedPermanentlyAt: raw.Failure.DeadAt,
		FailureKind:         raw.Failure.FailureCode,
		LastError:           raw.Failure.ErrorText,
	}
}

func localizedTextFromPayload(payload []byte) ([]value.LocalizedText, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var items []value.LocalizedText
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func stringSliceFromPayload(payload []byte) ([]string, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var items []string
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, err
	}
	return items, nil
}
