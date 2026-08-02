-- name: OwnerGateNextExpiredCandidate
SELECT
    id,
    organization_id,
    project_id,
    parent_id,
    owner_actor_id,
    kind,
    name,
    state,
    version,
    spec,
    created_at,
    updated_at,
    deleted_at
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND kind = 'OWNER_GATE'
  AND state = 'WAITING_OWNER'
  AND (spec ->> 'expiresAt')::timestamptz <= clock_timestamp()
ORDER BY (spec ->> 'expiresAt')::timestamptz, created_at, id
LIMIT 1
