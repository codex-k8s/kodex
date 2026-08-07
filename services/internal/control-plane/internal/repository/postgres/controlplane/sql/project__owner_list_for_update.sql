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
  AND project.state = 'ACTIVE'
  AND project.owner_actor_id = @actor_id::uuid
ORDER BY project.id
FOR UPDATE;
