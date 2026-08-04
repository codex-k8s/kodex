-- name: RuntimeRetentionHoldGetForUpdate :one
SELECT hold_id, organization_id, project_id, session_id, kind, state, version,
       actor_id, reason_code, created_at, updated_at,
       coalesce(released_at, 'epoch'::timestamptz)
FROM control_plane.runtime_retention_holds
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND hold_id = @hold_id::uuid
FOR UPDATE;
