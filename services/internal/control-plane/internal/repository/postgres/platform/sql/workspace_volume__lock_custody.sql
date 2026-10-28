-- name: workspace_volume__lock_custody :one
SELECT v.id::text
FROM control_plane.session_workspace_volume_tasks t
JOIN control_plane.session_workspace_volumes v ON v.id=t.volume_id
JOIN control_plane.session_workspace_storage w ON w.id=v.workspace_id
WHERE w.organization_id=@organization_id::uuid AND t.ref=@task_ref
FOR UPDATE OF v;
