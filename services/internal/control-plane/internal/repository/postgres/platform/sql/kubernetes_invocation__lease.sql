-- name: kubernetes_invocation__lease :one
SELECT lease_ref,generation,lease_expires_at
FROM control_plane.integration_invocations
WHERE organization_id=@organization_id::uuid AND ref=@invocation_ref AND state='RUNNING'
 AND claimed_workload='integration-gateway' AND workload_instance=@instance
 AND lease_ref=@lease_ref AND effect_fence_digest=@fence_digest AND generation=@generation AND input_digest=@input_digest
 AND lease_expires_at>clock_timestamp() FOR UPDATE;
