-- name: session__create :exec
INSERT INTO agent_manager_sessions (
    id, scope_type, scope_ref, provider_work_item_ref, flow_version_id,
    current_stage_id, latest_state_snapshot_id, status, created_by_actor_ref,
    version, created_at, updated_at
) VALUES (
    @id, @scope_type, @scope_ref, @provider_work_item_ref, @flow_version_id::uuid,
    @current_stage_id::uuid, @latest_state_snapshot_id::uuid, @status, @created_by_actor_ref,
    @version, @created_at, @updated_at
);
