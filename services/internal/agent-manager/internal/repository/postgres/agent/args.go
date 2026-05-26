package agent

import (
	"encoding/base64"
	"encoding/json"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
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
		"session_id":   acceptance.SessionID,
		"run_id":       postgreslib.NullableUUID(acceptance.RunID),
		"stage_id":     postgreslib.NullableUUID(acceptance.StageID),
		"check_kind":   string(acceptance.CheckKind),
		"status":       string(acceptance.Status),
		"target_ref":   acceptance.TargetRef,
		"details_json": objectPayload(acceptance.DetailsJSON),
	}, acceptance.ID, acceptance.Version, acceptance.CreatedAt, acceptance.UpdatedAt)
}

func acceptanceResultUpdateArgs(acceptance entity.AcceptanceResult, previousVersion int64) pgx.NamedArgs {
	args := acceptanceResultArgs(acceptance)
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

func encodePageToken(token string) string {
	if token == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(token))
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
