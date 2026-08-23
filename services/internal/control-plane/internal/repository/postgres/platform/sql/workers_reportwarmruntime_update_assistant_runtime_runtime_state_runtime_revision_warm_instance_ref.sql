-- name: workers_reportwarmruntime_update_assistant_runtime_runtime_state_runtime_revision_warm_instance_ref :one
WITH current AS (
    SELECT organization_id,
           runtime_state,
           runtime_revision,
           warm_instance_ref
    FROM control_plane.assistant_runtime
    WHERE organization_id = @organization_id::uuid
    FOR UPDATE
)
UPDATE control_plane.assistant_runtime runtime
SET runtime_state = @runtime_state,
    runtime_revision = @runtime_revision,
    warm_instance_ref = @workload_instance,
    last_heartbeat_at = clock_timestamp(),
    version = runtime.version + CASE
        WHEN current.runtime_state IS DISTINCT FROM @runtime_state
          OR current.runtime_revision IS DISTINCT FROM @runtime_revision
          OR current.warm_instance_ref IS DISTINCT FROM @workload_instance
        THEN 1
        ELSE 0
    END,
    updated_at = clock_timestamp()
FROM current
WHERE runtime.organization_id = current.organization_id
  AND runtime.desired_runtime_revision = @runtime_revision
  AND (current.warm_instance_ref IS NULL OR current.warm_instance_ref = @workload_instance)
RETURNING runtime.stable_key,
          runtime.core_prompt_revision,
          runtime.owner_instructions,
          runtime.runtime_state,
          runtime.runtime_revision,
          runtime.desired_runtime_revision,
          runtime.system_session_ref,
          runtime.resource_limits,
          runtime.last_heartbeat_at,
          runtime.version,
          runtime.updated_at,
          (current.runtime_state IS DISTINCT FROM @runtime_state
           OR current.runtime_revision IS DISTINCT FROM @runtime_revision
           OR current.warm_instance_ref IS DISTINCT FROM @workload_instance) AS changed;
