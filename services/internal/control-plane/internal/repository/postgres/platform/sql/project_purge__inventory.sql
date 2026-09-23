-- name: project_purge__inventory :many
SELECT kind,target,version
FROM control_plane.project_purge_external_inventory(@organization_id::uuid,@project_id::uuid)
ORDER BY kind,target,version
