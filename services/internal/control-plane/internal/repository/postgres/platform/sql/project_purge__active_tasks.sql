-- name: project_purge__active_tasks :one
SELECT
 (SELECT count(*) FROM control_plane.session_archive_tasks
  WHERE organization_id=$1::uuid AND project_id=$2::uuid AND state IN ('READY','CLAIMED'))
 + (SELECT count(*) FROM control_plane.session_workspace_storage
    WHERE organization_id=$1::uuid AND project_id=$2::uuid AND state<>'PURGED')
