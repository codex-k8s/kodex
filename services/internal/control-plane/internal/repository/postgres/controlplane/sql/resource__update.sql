UPDATE control_plane.resources
SET
    name = @name,
    state = @state,
    version = @new_version,
    spec = @spec::jsonb,
    schedule_next_run_at = @schedule_next_run_at,
    updated_at = @updated_at
WHERE id = @id::uuid
  AND organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND version = @expected_version
