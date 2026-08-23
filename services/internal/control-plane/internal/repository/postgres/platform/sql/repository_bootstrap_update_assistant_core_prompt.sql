-- name: repository_bootstrap_update_assistant_core_prompt :exec
UPDATE control_plane.assistant_runtime
SET core_prompt_ref = @prompt_ref,
    core_prompt_revision = @next_revision,
    desired_runtime_revision = @next_revision,
    runtime_state = 'RECOVERING',
    warm_instance_ref = NULL,
    last_heartbeat_at = NULL,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id::uuid
  AND agent_id = @agent_id::uuid
  AND core_prompt_revision = @current_revision;
