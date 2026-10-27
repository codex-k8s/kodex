-- name: workspace_archive__task_session :one
SELECT t.session_id::text
FROM control_plane.session_archive_tasks t
WHERE t.organization_id=@organization_id::uuid AND t.ref=@task_ref AND t.workspace_id IS NOT NULL;
