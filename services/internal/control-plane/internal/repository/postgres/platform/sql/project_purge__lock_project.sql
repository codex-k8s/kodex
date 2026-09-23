-- name: project_purge__lock_project :one
SELECT ref FROM control_plane.projects
WHERE organization_id=$1::uuid AND id=$2::uuid AND lifecycle='PURGE_PENDING'
FOR UPDATE
