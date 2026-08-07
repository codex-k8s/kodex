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
FROM control_plane.resources AS candidate
WHERE candidate.organization_id = @organization_id::uuid
  AND candidate.project_id = @project_id::uuid
  AND candidate.kind = @kind
  AND candidate.name = @name
  AND candidate.state <> 'DELETED'
  AND 1 = (
      SELECT count(*)
      FROM control_plane.resources AS matching
      WHERE matching.organization_id = @organization_id::uuid
        AND matching.project_id = @project_id::uuid
        AND matching.kind = @kind
        AND matching.name = @name
        AND matching.state <> 'DELETED'
  )
FOR UPDATE;
