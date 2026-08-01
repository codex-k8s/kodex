SELECT
    project.id::text,
    project.organization_id::text,
    project.project_id::text,
    coalesce(project.parent_id::text, ''),
    project.owner_actor_id::text,
    project.kind,
    project.name,
    project.state,
    project.version,
    project.spec,
    project.created_at,
    project.updated_at
FROM control_plane.resources AS project
WHERE project.organization_id = @organization_id::uuid
  AND project.kind = 'PROJECT'
  AND project.state <> 'DELETED'
  AND project.id > coalesce(nullif(@after_id, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
  AND EXISTS (
      SELECT 1
      FROM control_plane.project_actor_permissions AS permission
      WHERE permission.organization_id = project.organization_id
        AND permission.project_id = project.id
        AND permission.actor_id = @actor_id::uuid
        AND permission.permission IN ('*', 'controlplane.resource.list')
  )
ORDER BY project.id
LIMIT @limit
