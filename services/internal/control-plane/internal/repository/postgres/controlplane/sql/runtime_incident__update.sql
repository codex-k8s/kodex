UPDATE control_plane.runtime_execution_incidents
SET version = @version,
    execution_fence = @execution_fence,
    state = @state,
    action_reason_code = nullif(@reason_code, ''),
    updated_at = @updated_at
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND id = @incident_id::uuid
  AND version = @expected_version;
