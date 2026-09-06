-- name: prompt_impact_lock_consumer :one
WITH agent AS (
 SELECT version FROM control_plane.agents WHERE organization_id=@organization_id::uuid AND ref=@consumer_ref
 AND @consumer_kind IN ('AGENT','AGENT_CONTINUATION') AND state<>'ARCHIVED' FOR UPDATE
), workflow AS (
 SELECT version FROM control_plane.workflows WHERE organization_id=@organization_id::uuid AND ref=@consumer_ref
 AND @consumer_kind='WORKFLOW' AND state<>'ARCHIVED' FOR UPDATE
), schedule AS (
 SELECT version FROM control_plane.schedules WHERE organization_id=@organization_id::uuid AND ref=@consumer_ref
 AND @consumer_kind='SCHEDULE' AND lifecycle_state<>'ARCHIVED' FOR UPDATE
), binding AS (
 SELECT version FROM control_plane.managed_configuration_bindings WHERE organization_id=@organization_id::uuid
 AND ref=@binding_ref AND configuration_kind='PROMPT_TEMPLATE' AND consumer_kind=@consumer_kind AND consumer_ref=@consumer_ref FOR UPDATE
)
SELECT COALESCE((SELECT version FROM agent),(SELECT version FROM workflow),(SELECT version FROM schedule)),version FROM binding;
