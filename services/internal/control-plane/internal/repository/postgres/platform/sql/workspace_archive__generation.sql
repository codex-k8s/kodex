-- name: workspace_archive__generation :one
SELECT GREATEST(
 COALESCE((SELECT content_generation FROM control_plane.session_storage WHERE session_id=@session_id::uuid),0),
 COALESCE((SELECT max(content_generation) FROM control_plane.session_archives WHERE session_id=@session_id::uuid),0),
 COALESCE((SELECT max(content_generation) FROM control_plane.session_archive_tasks WHERE session_id=@session_id::uuid),0));
