-- name: runtime_configuration__bind_environment :one
WITH resolved AS (
    SELECT environment.id, environment.ref
    FROM control_plane.runtime_environment_sets environment
    WHERE environment.organization_id = @organization_id::uuid
      AND environment.ref = @environment_ref
      AND environment.project_id = @project_id::uuid
      AND environment.state = 'ACTIVE'
), updated AS (
    UPDATE control_plane.agent_runtime_environment_bindings binding
    SET environment_set_id = resolved.id,
        version = binding.version + 1,
        digest = @digest,
        updated_by = @updated_by::uuid,
        updated_at = clock_timestamp()
    FROM resolved
    WHERE binding.agent_id = @agent_id::uuid
      AND binding.version = @expected_version
    RETURNING binding.ref
)
SELECT ref FROM updated;
