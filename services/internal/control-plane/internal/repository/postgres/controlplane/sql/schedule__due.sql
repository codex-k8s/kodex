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
  AND project_id = @project_id::uuid
  AND kind = 'SCHEDULE'
  AND state = 'ACTIVE'
  AND schedule_next_run_at <= @now
ORDER BY schedule_next_run_at, id
FOR UPDATE SKIP LOCKED
LIMIT @limit
