package agent

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
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

type activityPageQueryArgs struct {
	pgx.NamedArgs
	PageSize int32
}

type activityPageCursor struct {
	StartedAt time.Time
	ID        uuid.UUID
}

type activityPageToken struct {
	StartedAt string `json:"started_at"`
	ID        string `json:"id"`
}

func flowArgs(flow entity.Flow) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"scope_type":        flow.Scope.Type,
		"scope_ref":         flow.Scope.Ref,
		"slug":              flow.Slug,
		"display_name":      jsonArrayPayload(flow.DisplayName),
		"description":       jsonArrayPayload(flow.Description),
		"icon_object_uri":   flow.IconObjectURI,
		"status":            string(flow.Status),
		"active_version_id": postgreslib.NullableUUID(flow.ActiveVersionID),
	}, flow.ID, flow.Version, flow.CreatedAt, flow.UpdatedAt)
}

func flowUpdateArgs(flow entity.Flow, previousVersion int64) pgx.NamedArgs {
	args := flowArgs(flow)
	args["previous_version"] = previousVersion
	return args
}

func flowVersionArgs(version entity.FlowVersion) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                version.ID,
		"flow_id":           version.FlowID,
		"version":           version.Version,
		"source_ref":        version.SourceRef,
		"definition_digest": version.DefinitionDigest,
		"status":            string(version.Status),
		"activated_at":      postgreslib.NullableTime(version.ActivatedAt),
		"created_at":        version.CreatedAt,
	}
}

func stageArgs(stage entity.Stage) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                 stage.ID,
		"flow_version_id":    stage.FlowVersionID,
		"slug":               stage.Slug,
		"stage_type":         string(stage.StageType),
		"display_name":       jsonArrayPayload(stage.DisplayName),
		"icon_object_uri":    stage.IconObjectURI,
		"required_artifacts": objectPayload(stage.RequiredArtifactsJSON),
		"acceptance_policy":  objectPayload(stage.AcceptancePolicyJSON),
		"position":           stage.Position,
	}
}

func stageTransitionArgs(transition entity.StageTransition) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                transition.ID,
		"flow_version_id":   transition.FlowVersionID,
		"from_stage_id":     postgreslib.NullableUUID(transition.FromStageID),
		"to_stage_id":       transition.ToStageID,
		"condition_payload": objectPayload(transition.ConditionJSON),
		"follow_up_type":    transition.FollowUpType,
		"position":          transition.Position,
	}
}

func stageRoleBindingArgs(binding entity.StageRoleBinding) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                      binding.ID,
		"stage_id":                binding.StageID,
		"role_profile_id":         binding.RoleProfileID,
		"binding_kind":            string(binding.BindingKind),
		"launch_policy":           objectPayload(binding.LaunchPolicyJSON),
		"required_for_acceptance": binding.RequiredForAcceptance,
	}
}

func roleProfileArgs(role entity.RoleProfile) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"scope_type":                  role.Scope.Type,
		"scope_ref":                   role.Scope.Ref,
		"slug":                        role.Slug,
		"display_name":                jsonArrayPayload(role.DisplayName),
		"icon_object_uri":             role.IconObjectURI,
		"role_kind":                   string(role.RoleKind),
		"runtime_profile":             role.RuntimeProfile,
		"allowed_mcp_tools":           jsonArrayPayload(role.AllowedMCPTools),
		"provider_account_policy_ref": role.ProviderAccountPolicyRef,
		"status":                      string(role.Status),
	}, role.ID, role.Version, role.CreatedAt, role.UpdatedAt)
}

func roleProfileUpdateArgs(role entity.RoleProfile, previousVersion int64) pgx.NamedArgs {
	args := roleProfileArgs(role)
	args["previous_version"] = previousVersion
	return args
}

func promptTemplateArgs(template entity.PromptTemplate) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"role_profile_id":   template.RoleProfileID,
		"prompt_kind":       string(template.PromptKind),
		"active_version_id": postgreslib.NullableUUID(template.ActiveVersionID),
	}, template.ID, template.Version, template.CreatedAt, template.UpdatedAt)
}

func promptTemplateUpdateArgs(template entity.PromptTemplate, previousVersion int64) pgx.NamedArgs {
	args := promptTemplateArgs(template)
	args["previous_version"] = previousVersion
	return args
}

func promptTemplateVersionArgs(version entity.PromptTemplateVersion) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                         version.ID,
		"prompt_template_id":         version.PromptTemplateID,
		"role_profile_id":            version.RoleProfileID,
		"prompt_kind":                string(version.PromptKind),
		"version":                    version.Version,
		"source_ref":                 version.SourceRef,
		"template_object_uri":        version.TemplateObject.ObjectURI,
		"template_object_digest":     version.TemplateObject.ObjectDigest,
		"template_object_size_bytes": version.TemplateObject.ObjectSizeBytes,
		"template_digest":            version.TemplateDigest,
		"status":                     string(version.Status),
		"activated_at":               postgreslib.NullableTime(version.ActivatedAt),
		"created_at":                 version.CreatedAt,
	}
}

func agentSessionArgs(session entity.AgentSession) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"scope_type":               session.Scope.Type,
		"scope_ref":                session.Scope.Ref,
		"provider_work_item_ref":   session.ProviderWorkItemRef,
		"flow_version_id":          postgreslib.NullableUUID(session.FlowVersionID),
		"current_stage_id":         postgreslib.NullableUUID(session.CurrentStageID),
		"latest_state_snapshot_id": postgreslib.NullableUUID(session.LatestStateSnapshotID),
		"status":                   string(session.Status),
		"created_by_actor_ref":     session.CreatedByActorRef,
	}, session.ID, session.Version, session.CreatedAt, session.UpdatedAt)
}

func agentSessionUpdateArgs(session entity.AgentSession, previousVersion int64) pgx.NamedArgs {
	args := agentSessionArgs(session)
	args["previous_version"] = previousVersion
	return args
}

func agentRunArgs(run entity.AgentRun) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"session_id":                 run.SessionID,
		"flow_version_id":            postgreslib.NullableUUID(run.FlowVersionID),
		"stage_id":                   postgreslib.NullableUUID(run.StageID),
		"role_profile_id":            run.RoleProfileID,
		"role_profile_version":       run.RoleProfileVersion,
		"role_profile_digest":        run.RoleProfileDigest,
		"prompt_template_version_id": run.PromptTemplateVersionID,
		"prompt_template_digest":     run.PromptTemplateDigest,
		"runtime_context":            jsonObjectPayload(run.RuntimeContext),
		"provider_target":            jsonObjectPayload(run.ProviderTarget),
		"guidance_refs":              jsonArrayPayload(run.GuidanceRefs),
		"status":                     string(run.Status),
		"result_summary":             run.ResultSummary,
		"failure_code":               run.FailureCode,
		"started_at":                 postgreslib.NullableTime(run.StartedAt),
		"finished_at":                postgreslib.NullableTime(run.FinishedAt),
	}, run.ID, run.Version, run.CreatedAt, run.UpdatedAt)
}

func agentRunUpdateArgs(run entity.AgentRun, previousVersion int64) pgx.NamedArgs {
	args := agentRunArgs(run)
	args["previous_version"] = previousVersion
	return args
}

func sessionStateSnapshotArgs(snapshot entity.AgentSessionStateSnapshot) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                snapshot.ID,
		"session_id":        snapshot.SessionID,
		"run_id":            postgreslib.NullableUUID(snapshot.RunID),
		"snapshot_kind":     string(snapshot.SnapshotKind),
		"turn_index":        snapshot.TurnIndex,
		"object_uri":        snapshot.Object.ObjectURI,
		"object_digest":     snapshot.Object.ObjectDigest,
		"object_size_bytes": snapshot.Object.ObjectSizeBytes,
		"captured_at":       snapshot.CapturedAt,
		"created_at":        snapshot.CreatedAt,
	}
}

func acceptanceResultArgs(acceptance entity.AcceptanceResult) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"session_id":                     acceptance.SessionID,
		"run_id":                         postgreslib.NullableUUID(acceptance.RunID),
		"stage_id":                       postgreslib.NullableUUID(acceptance.StageID),
		"check_kind":                     string(acceptance.CheckKind),
		"status":                         string(acceptance.Status),
		"target_ref":                     acceptance.TargetRef,
		"details_json":                   objectPayload(acceptance.DetailsJSON),
		"governance_risk_assessment_ref": acceptance.GovernanceContext.RiskAssessmentRef,
		"governance_gate_request_ref":    acceptance.GovernanceContext.GateRequestRef,
		"governance_gate_decision_ref":   acceptance.GovernanceContext.GateDecisionRef,
		"governance_release_decision_package_ref": acceptance.GovernanceContext.ReleaseDecisionPackageRef,
		"governance_release_decision_ref":         acceptance.GovernanceContext.ReleaseDecisionRef,
		"governance_risk_profile_ref":             acceptance.GovernanceContext.RiskProfileRef,
		"governance_gate_policy_ref":              acceptance.GovernanceContext.GatePolicyRef,
		"governance_release_policy_ref":           acceptance.GovernanceContext.ReleasePolicyRef,
	}, acceptance.ID, acceptance.Version, acceptance.CreatedAt, acceptance.UpdatedAt)
}

func acceptanceResultUpdateArgs(acceptance entity.AcceptanceResult, previousVersion int64) pgx.NamedArgs {
	args := acceptanceResultArgs(acceptance)
	args["previous_version"] = previousVersion
	return args
}

func followUpIntentArgs(intent entity.FollowUpIntent) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"session_id":                              intent.SessionID,
		"run_id":                                  postgreslib.NullableUUID(intent.RunID),
		"from_stage_id":                           postgreslib.NullableUUID(intent.FromStageID),
		"to_stage_id":                             postgreslib.NullableUUID(intent.ToStageID),
		"acceptance_result_id":                    postgreslib.NullableUUID(intent.AcceptanceResultID),
		"provider_work_item_ref":                  intent.ProviderTarget.WorkItemRef,
		"provider_pull_request_ref":               intent.ProviderTarget.PullRequestRef,
		"provider_comment_ref":                    intent.ProviderTarget.CommentRef,
		"provider_review_signal_ref":              intent.ProviderTarget.ReviewSignalRef,
		"provider_work_item_type":                 intent.ProviderWorkItemType,
		"provider_operation_ref":                  intent.ProviderOperationRef,
		"instruction_body_digest":                 intent.InstructionBodyDigest,
		"safe_title":                              intent.SafeTitle,
		"safe_summary":                            intent.SafeSummary,
		"role_hint":                               intent.RoleHint,
		"stage_hint":                              intent.StageHint,
		"idempotency_key":                         intent.IdempotencyKey,
		"governance_risk_assessment_ref":          intent.GovernanceContext.RiskAssessmentRef,
		"governance_gate_request_ref":             intent.GovernanceContext.GateRequestRef,
		"governance_gate_decision_ref":            intent.GovernanceContext.GateDecisionRef,
		"governance_release_decision_package_ref": intent.GovernanceContext.ReleaseDecisionPackageRef,
		"governance_release_decision_ref":         intent.GovernanceContext.ReleaseDecisionRef,
		"governance_risk_profile_ref":             intent.GovernanceContext.RiskProfileRef,
		"governance_gate_policy_ref":              intent.GovernanceContext.GatePolicyRef,
		"governance_release_policy_ref":           intent.GovernanceContext.ReleasePolicyRef,
		"status":                                  string(intent.Status),
	}, intent.ID, intent.Version, intent.CreatedAt, intent.UpdatedAt)
}

func followUpIntentUpdateArgs(intent entity.FollowUpIntent, previousVersion int64) pgx.NamedArgs {
	args := followUpIntentArgs(intent)
	args["previous_version"] = previousVersion
	return args
}

func agentActivityArgs(activity entity.AgentActivity) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"session_id":        activity.SessionID,
		"run_id":            postgreslib.NullableUUID(activity.RunID),
		"turn_id":           activity.TurnID,
		"tool_use_id":       activity.ToolUseID,
		"activity_kind":     string(activity.ActivityKind),
		"tool_name":         activity.ToolName,
		"tool_category":     activity.ToolCategory,
		"status":            string(activity.Status),
		"started_at":        activity.StartedAt,
		"finished_at":       postgreslib.NullableTime(activity.FinishedAt),
		"duration_ms":       optionalInt64(activity.DurationMs),
		"safe_summary":      activity.SafeSummary,
		"payload_digest":    activity.PayloadDigest,
		"bounded_error":     activity.BoundedError,
		"safe_refs_json":    objectPayload(activity.SafeRefsJSON),
		"safe_details_json": objectPayload(activity.SafeDetailsJSON),
		"correlation_id":    activity.CorrelationID,
		"idempotency_key":   activity.IdempotencyKey,
	}, activity.ID, activity.Version, activity.CreatedAt, activity.UpdatedAt)
}

func humanGateRequestArgs(gate entity.HumanGateRequest) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(pgx.NamedArgs{
		"session_id":                              gate.SessionID,
		"run_id":                                  postgreslib.NullableUUID(gate.RunID),
		"stage_id":                                postgreslib.NullableUUID(gate.StageID),
		"acceptance_result_id":                    postgreslib.NullableUUID(gate.AcceptanceResultID),
		"provider_work_item_ref":                  gate.ProviderTarget.WorkItemRef,
		"provider_pull_request_ref":               gate.ProviderTarget.PullRequestRef,
		"provider_comment_ref":                    gate.ProviderTarget.CommentRef,
		"provider_review_signal_ref":              gate.ProviderTarget.ReviewSignalRef,
		"target_ref":                              gate.TargetRef,
		"request_kind":                            gate.RequestKind,
		"reason_code":                             gate.ReasonCode,
		"safe_summary":                            gate.SafeSummary,
		"interaction_request_ref":                 gate.InteractionRequestRef,
		"interaction_response_ref":                gate.InteractionResponseRef,
		"governance_gate_request_ref":             gate.GovernanceGateRequestRef,
		"governance_decision_ref":                 gate.GovernanceDecisionRef,
		"governance_risk_assessment_ref":          gate.GovernanceContext.RiskAssessmentRef,
		"governance_release_decision_package_ref": gate.GovernanceContext.ReleaseDecisionPackageRef,
		"governance_release_decision_ref":         gate.GovernanceContext.ReleaseDecisionRef,
		"governance_risk_profile_ref":             gate.GovernanceContext.RiskProfileRef,
		"governance_gate_policy_ref":              gate.GovernanceContext.GatePolicyRef,
		"governance_release_policy_ref":           gate.GovernanceContext.ReleasePolicyRef,
		"idempotency_key":                         gate.IdempotencyKey,
		"status":                                  string(gate.Status),
		"outcome":                                 string(gate.Outcome),
		"resolved_at":                             postgreslib.NullableTime(gate.ResolvedAt),
	}, gate.ID, gate.Version, gate.CreatedAt, gate.UpdatedAt)
}

func humanGateRequestUpdateArgs(gate entity.HumanGateRequest, previousVersion int64) pgx.NamedArgs {
	args := humanGateRequestArgs(gate)
	args["previous_version"] = previousVersion
	return args
}

func commandResultArgs(result entity.CommandResult) pgx.NamedArgs {
	args := pgx.NamedArgs{"key": result.Key}
	args["command_id"] = postgreslib.NullableUUID(result.CommandID)
	args["idempotency_key"] = result.IdempotencyKey
	args["actor_type"] = result.Actor.Type
	args["actor_id"] = result.Actor.ID
	args["operation"] = result.Operation
	args["aggregate_type"] = string(result.AggregateType)
	args["aggregate_id"] = result.AggregateID
	args["result_payload"] = objectPayload(result.ResultPayload)
	args["created_at"] = result.CreatedAt
	return args
}

func commandIdentityArgs(identity query.CommandIdentity) pgx.NamedArgs {
	args := pgx.NamedArgs{
		"command_id":      postgreslib.NullableUUID(identity.CommandID),
		"idempotency_key": identity.IdempotencyKey,
		"operation":       identity.Operation,
	}
	addCommandActorArgs(args, identity.Actor)
	return args
}

func addCommandActorArgs(args pgx.NamedArgs, actor value.Actor) {
	args["actor_type"] = actor.Type
	args["actor_id"] = actor.ID
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

func flowFilterArgs(filter query.FlowFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"scope_type": optionalString(filter.Scope.Type),
		"scope_ref":  optionalString(filter.Scope.Ref),
		"status":     optionalEnum(filter.Status),
	})
}

func flowVersionFilterArgs(filter query.FlowVersionFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"flow_id": filter.FlowID,
		"status":  optionalEnum(filter.Status),
	})
}

func roleProfileFilterArgs(filter query.RoleProfileFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"scope_type": optionalString(filter.Scope.Type),
		"scope_ref":  optionalString(filter.Scope.Ref),
		"role_kind":  optionalEnum(filter.Kind),
		"status":     optionalEnum(filter.Status),
	})
}

func promptTemplateFilterArgs(filter query.PromptTemplateFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"role_profile_id": filter.RoleProfileID,
		"prompt_kind":     optionalEnum(filter.Kind),
	})
}

func promptTemplateVersionFilterArgs(filter query.PromptTemplateVersionFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"role_profile_id": filter.RoleProfileID,
		"prompt_kind":     optionalEnum(filter.Kind),
		"status":          optionalEnum(filter.Status),
	})
}

func agentRunFilterArgs(filter query.AgentRunFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"session_id":             optionalUUID(filter.SessionID),
		"role_profile_id":        optionalUUID(filter.RoleProfileID),
		"status":                 optionalEnum(filter.Status),
		"provider_work_item_ref": optionalString(filter.ProviderWorkItemRef),
	})
}

func acceptanceResultFilterArgs(filter query.AcceptanceResultFilter) pageQueryArgs {
	args := pgx.NamedArgs{}
	args["session_id"] = optionalUUID(filter.SessionID)
	args["run_id"] = optionalUUID(filter.RunID)
	args["stage_id"] = optionalUUID(filter.StageID)
	args["status"] = optionalEnum(filter.Status)
	return withPage(filter.Page, args)
}

func humanGateRequestFilterArgs(filter query.HumanGateFilter) pageQueryArgs {
	args := pgx.NamedArgs{}
	args["session_id"] = optionalUUID(filter.SessionID)
	args["run_id"] = optionalUUID(filter.RunID)
	args["stage_id"] = optionalUUID(filter.StageID)
	args["status"] = optionalEnum(filter.Status)
	args["outcome"] = optionalEnum(filter.Outcome)
	return withPage(filter.Page, args)
}

func agentActivityFilterArgs(filter query.AgentActivityFilter) (activityPageQueryArgs, error) {
	cursor, err := decodeActivityPageToken(filter.Page.PageToken)
	if err != nil {
		return activityPageQueryArgs{}, err
	}
	args := pgx.NamedArgs{
		"activity_kind":     optionalEnum(filter.ActivityKind),
		"activity_status":   optionalEnum(filter.Status),
		"cursor_started_at": nil,
		"cursor_id":         nil,
	}
	if cursor != nil {
		args["cursor_started_at"] = cursor.StartedAt
		args["cursor_id"] = cursor.ID
	}
	limit := pageSize(filter.Page)
	args["session_id"] = optionalUUID(filter.SessionID)
	args["run_id"] = optionalUUID(filter.RunID)
	args["limit"] = limit + 1
	return activityPageQueryArgs{NamedArgs: args, PageSize: limit}, nil
}

func withPage(page value.PageRequest, args pgx.NamedArgs) pageQueryArgs {
	limit, offset, _ := postgreslib.OffsetPageBounds(page.PageSize, decodePageToken(page.PageToken), defaultPageSize, maxPageSize)
	args["limit"] = limit + 1
	args["offset"] = offset
	return pageQueryArgs{NamedArgs: args, PageSize: limit, Offset: offset}
}

func pageResult[T any](items []T, page pageQueryArgs) ([]T, value.PageResult) {
	trimmed, next := postgreslib.TrimOffsetPage(items, page.PageSize, page.Offset+page.PageSize)
	return trimmed, value.PageResult{NextPageToken: encodePageToken(next)}
}

func activityPageResult(items []entity.AgentActivity, page activityPageQueryArgs) ([]entity.AgentActivity, value.PageResult) {
	if int32(len(items)) <= page.PageSize {
		return items, value.PageResult{}
	}
	trimmed := items[:int(page.PageSize)]
	next := encodeActivityPageToken(trimmed[len(trimmed)-1])
	return trimmed, value.PageResult{NextPageToken: next}
}

func pageSize(page value.PageRequest) int32 {
	limit, _, _ := postgreslib.OffsetPageBounds(page.PageSize, "", defaultPageSize, maxPageSize)
	return limit
}

func encodePageToken(token string) string {
	if token == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(token))
}

func encodeActivityPageToken(activity entity.AgentActivity) string {
	payload, err := json.Marshal(activityPageToken{
		StartedAt: activity.StartedAt.UTC().Format(time.RFC3339Nano),
		ID:        activity.ID.String(),
	})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func optionalString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func optionalUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func optionalInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalEnum[T ~string](value *T) any {
	if value == nil || *value == "" {
		return nil
	}
	return string(*value)
}

func jsonArrayPayload(value any) string {
	payload, err := json.Marshal(value)
	if err != nil || string(payload) == "null" {
		return "[]"
	}
	return string(payload)
}

func jsonObjectPayload(value any) string {
	payload, err := json.Marshal(value)
	if err != nil || string(payload) == "null" {
		return "{}"
	}
	return string(payload)
}

func objectPayload(payload []byte) string {
	return postgreslib.JSONPayload(payload)
}

func decodePageToken(token string) string {
	if token == "" {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return ""
	}
	_, err = strconv.ParseInt(string(decoded), 10, 64)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func decodeActivityPageToken(token string) (*activityPageCursor, error) {
	if token == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	var payload activityPageToken
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	startedAt, err := time.Parse(time.RFC3339Nano, payload.StartedAt)
	if err != nil || startedAt.IsZero() {
		return nil, errs.ErrInvalidArgument
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil || id == uuid.Nil {
		return nil, errs.ErrInvalidArgument
	}
	return &activityPageCursor{StartedAt: startedAt.UTC(), ID: id}, nil
}
