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
FROM control_plane.runtime_derived_resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND id = @resource_id::uuid
  AND kind = @kind
  AND version = @version
