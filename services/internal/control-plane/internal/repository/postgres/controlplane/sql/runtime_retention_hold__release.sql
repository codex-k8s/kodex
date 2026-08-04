-- name: RuntimeRetentionHoldRelease :exec
UPDATE control_plane.runtime_retention_holds
SET state = 'RELEASED', version = version + 1,
    reason_code = @reason_code, updated_at = @released_at,
    released_at = @released_at
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND hold_id = @hold_id::uuid
  AND session_id = @session_id::uuid
  AND version = @expected_version
  AND state = 'ACTIVE';
