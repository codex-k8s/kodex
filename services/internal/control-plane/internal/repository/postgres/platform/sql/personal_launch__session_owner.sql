-- name: personal_launch__session_owner :one
SELECT created_by::text
FROM control_plane.sessions
WHERE organization_id=@organization_id::uuid AND project_id=@project_id::uuid
  AND ref=@session_ref AND target_type=@target_type AND target_ref=@target_ref
FOR SHARE;
