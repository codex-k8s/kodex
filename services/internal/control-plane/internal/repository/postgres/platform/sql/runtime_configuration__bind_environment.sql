-- name: runtime_configuration__bind_environment :one
WITH resolved AS (
    SELECT environment.id, environment.ref, revision.id AS version_id
    FROM control_plane.runtime_environment_sets environment
    JOIN control_plane.runtime_environment_versions revision
      ON revision.environment_set_id = environment.id
     AND revision.organization_id = environment.organization_id
     AND ((@version_ref = '' AND revision.id = environment.current_version_id) OR revision.ref = @version_ref)
    WHERE environment.organization_id = @organization_id::uuid
      AND environment.ref = @environment_ref
      AND environment.project_id = @project_id::uuid
      AND environment.state = 'ACTIVE'
), updated AS (
    UPDATE control_plane.agent_runtime_environment_bindings binding
    SET environment_set_id = resolved.id,
        environment_version_id = resolved.version_id,
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
