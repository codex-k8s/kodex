-- name: PermissionIndexActorList
SELECT permission
FROM control_plane.project_actor_permissions
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND actor_id = @actor_id::uuid
ORDER BY permission
