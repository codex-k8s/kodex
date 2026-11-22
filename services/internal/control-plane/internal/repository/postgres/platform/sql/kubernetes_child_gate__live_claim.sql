-- name: kubernetes_child_gate__live_claim :one
SELECT invocation.lease_ref,invocation.generation,invocation.effect_fence_digest,invocation.lease_expires_at
FROM control_plane.integration_invocations invocation
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.state='RUNNING' AND invocation.resource_kind='KUBERNETES_TARGET'
 AND invocation.claimed_workload='integration-gateway' AND invocation.workload_instance IS NOT NULL
 AND invocation.lease_ref IS NOT NULL AND invocation.generation>0 AND invocation.effect_fence_digest IS NOT NULL
 AND invocation.lease_expires_at>clock_timestamp()
FOR UPDATE;
