-- name: workspace_volume__report_origin :one
-- Durable exact fence precedes root resolution and any deadline/cancellation effect.
SELECT t.root_run_id::text
FROM control_plane.session_workspace_volume_tasks t
JOIN control_plane.session_workspace_volume_attempts a ON a.task_id=t.id
JOIN control_plane.session_workspace_volumes v ON v.id=t.volume_id
JOIN control_plane.session_workspace_storage w ON w.id=v.workspace_id
WHERE w.organization_id=@organization_id::uuid AND t.ref=@task_ref
 AND a.lease_ref=@lease_ref AND a.attempt=@generation::integer AND a.fence_digest=@fence_digest;
