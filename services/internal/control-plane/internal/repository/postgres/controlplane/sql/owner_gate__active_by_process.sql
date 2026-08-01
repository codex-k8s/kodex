-- name: OwnerGateActiveByProcess
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
  AND kind = 'OWNER_GATE'
  AND parent_id = @process_run_id::uuid
  AND state = 'WAITING_OWNER'
ORDER BY id
LIMIT 1
FOR UPDATE
