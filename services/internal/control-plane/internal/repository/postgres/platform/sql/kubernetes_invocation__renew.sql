-- name: kubernetes_invocation__renew :one
UPDATE control_plane.integration_invocations SET lease_expires_at=@expires_at,version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND ref=@invocation_ref AND state='RUNNING'
 AND claimed_workload='integration-gateway' AND workload_instance=@instance
 AND lease_ref=@lease_ref AND effect_fence_digest=@fence_digest AND generation=@generation AND input_digest=@input_digest
 AND lease_expires_at>clock_timestamp()
RETURNING lease_ref,generation,lease_expires_at;
