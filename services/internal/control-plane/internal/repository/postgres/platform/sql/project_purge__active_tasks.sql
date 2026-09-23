-- name: project_purge__active_tasks :one
SELECT count(*) FROM control_plane.session_archive_tasks
WHERE organization_id=$1::uuid AND project_id=$2::uuid AND state IN ('READY','CLAIMED')
