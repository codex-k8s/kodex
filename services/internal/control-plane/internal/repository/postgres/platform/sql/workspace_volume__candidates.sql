-- name: workspace_volume__candidates :many
SELECT t.ref,t.root_run_id::text
FROM control_plane.session_workspace_volume_tasks t
JOIN control_plane.session_workspace_volumes v ON v.id=t.volume_id AND v.generation=t.volume_generation
JOIN control_plane.session_workspace_storage w ON w.id=v.workspace_id
WHERE w.organization_id=@organization_id::uuid AND t.available_at<=clock_timestamp()
 AND (t.state='READY' OR t.state='CLAIMED' AND NOT EXISTS(
   SELECT 1 FROM control_plane.session_workspace_volume_attempts a
   WHERE a.task_id=t.id AND a.grant_state='ACTIVE' AND a.expires_at>clock_timestamp()))
ORDER BY t.available_at,t.id LIMIT @limit::integer;
