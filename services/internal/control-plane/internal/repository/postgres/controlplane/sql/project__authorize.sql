WITH target AS (
    SELECT resource.project_id
    FROM control_plane.resources AS resource
    WHERE resource.organization_id = @organization_id::uuid
      AND resource.project_id = @project_id::uuid
      AND resource.id = coalesce(
          nullif(@resource_reference, '')::uuid,
          @project_id::uuid
      )
      AND resource.state <> 'DELETED'
),
allowed AS (
    SELECT 1
    FROM control_plane.project_actor_permissions AS permission
    WHERE permission.organization_id = @organization_id::uuid
      AND permission.project_id = @project_id::uuid
      AND permission.actor_id = @actor_id::uuid
      AND permission.permission IN ('*', @permission)
)
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
  AND project.id = @project_id::uuid
  AND project.kind = 'PROJECT'
  AND project.state = 'ACTIVE'
  AND EXISTS (SELECT 1 FROM target)
  AND EXISTS (SELECT 1 FROM allowed)
