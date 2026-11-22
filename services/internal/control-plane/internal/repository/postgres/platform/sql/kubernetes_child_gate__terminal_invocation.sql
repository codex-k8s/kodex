-- name: kubernetes_child_gate__terminal_invocation :exec
UPDATE control_plane.integration_invocations
SET state=@state,safe_error_code=@safe_error_code,
 lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
 version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@invocation_id::uuid
 AND resource_kind='KUBERNETES_TARGET' AND state='RUNNING'
 AND lease_ref=@lease_ref AND generation=@generation AND effect_fence_digest=@fence_digest;
