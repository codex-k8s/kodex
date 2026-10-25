-- name: repository_bundle__renew_invocation :exec
UPDATE control_plane.integration_invocations
SET lease_expires_at=@expires_at,version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@invocation_id::uuid
  AND operation='github.repository.bundle.publish' AND state='RUNNING'
  AND lease_ref=@lease_ref AND effect_fence_digest=@fence_digest AND generation=@generation;
