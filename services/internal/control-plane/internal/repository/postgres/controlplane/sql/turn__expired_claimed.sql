SELECT
    resource.id::text,
    resource.organization_id::text,
    resource.project_id::text,
    coalesce(resource.parent_id::text, ''),
    resource.owner_actor_id::text,
    resource.kind,
    resource.name,
    resource.state,
    resource.version,
    resource.spec,
    resource.created_at,
    resource.updated_at,
    lease.turn_id::text,
    lease.token_hash,
    lease.workload_id,
    lease.authority_generation,
    lease.attempt,
    lease.expires_at,
    lease.fence
FROM control_plane.resources AS resource
JOIN control_plane.turn_leases AS lease
  ON lease.turn_id = resource.id
JOIN control_plane.resources AS session
  ON session.id = (resource.spec ->> 'sessionId')::uuid
 AND session.organization_id = resource.organization_id
 AND session.project_id = resource.project_id
 AND session.kind = 'SESSION'
 AND session.state = 'ACTIVE'
WHERE resource.organization_id = @organization_id::uuid
  AND resource.project_id = @project_id::uuid
  AND resource.id = @turn_id::uuid
  AND resource.kind = 'TURN'
  AND resource.state = 'CLAIMED'
  AND lease.expires_at <= @now
ORDER BY lease.expires_at, resource.id
FOR UPDATE OF resource, lease SKIP LOCKED
LIMIT @limit
