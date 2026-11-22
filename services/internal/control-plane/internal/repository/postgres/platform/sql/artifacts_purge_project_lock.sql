-- name: artifacts_purge_project_lock :one
SELECT id::text FROM control_plane.projects
WHERE id=@project_id::uuid AND organization_id=@organization_id::uuid
 AND lifecycle IN('ACTIVE','TRASHED')
FOR SHARE;
