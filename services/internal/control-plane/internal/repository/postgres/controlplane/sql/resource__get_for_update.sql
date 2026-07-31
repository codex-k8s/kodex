SELECT
    id::text,
    organization_id::text,
    project_id::text,
    coalesce(parent_id::text, ''),
    owner_actor_id::text,
    kind,
    name,
    state,
    version,
    spec,
    created_at,
    updated_at
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND id = @resource_id::uuid
  AND state <> 'DELETED'
  AND (
      project_id = nullif(@project_id, '')::uuid
      OR (@project_id = '' AND kind = 'PROJECT')
  )
FOR UPDATE
