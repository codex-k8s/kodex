-- name: kubernetes_invocation__cancel_lock :one
SELECT invocation.id::text FROM control_plane.integration_invocations invocation
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.resource_kind='KUBERNETES_TARGET' AND invocation.input_digest=@input_digest
 AND invocation.generation=@generation AND invocation.claimed_workload='integration-gateway'
 AND invocation.claimed_lease_ref=@lease_ref AND invocation.claimed_fence_digest=@fence_digest
 AND invocation.kubernetes_claimed_instance=@instance FOR UPDATE;
