SELECT
    id::text, organization_id::text, project_id::text,
    coalesce(parent_id::text, ''), owner_actor_id::text,
    kind, name, state, version, spec, created_at, updated_at
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND kind = 'IMAGE_BUILD'
  AND state = 'QUEUED'
  AND (spec ->> 'availableAt')::timestamptz <= @now
ORDER BY (spec ->> 'availableAt')::timestamptz, created_at, id
FOR UPDATE SKIP LOCKED
LIMIT 1
