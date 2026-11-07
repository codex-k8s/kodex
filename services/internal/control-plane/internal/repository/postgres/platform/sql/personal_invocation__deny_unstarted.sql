-- name: personal_invocation__deny_unstarted :exec
UPDATE control_plane.integration_invocations invocation
SET state='FAILED',safe_error_code='INTEGRATION_CREDENTIAL_UNAVAILABLE',
 lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
 version=invocation.version+1,updated_at=clock_timestamp()
FROM control_plane.integration_connections connection
WHERE invocation.connection_id=connection.id AND connection.organization_id=invocation.organization_id
 AND connection.credential_policy='INITIATOR_REQUIRED'
 AND invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.state='RUNNING' AND invocation.generation=@generation::bigint
 AND invocation.lease_ref=@lease_ref AND invocation.claimed_lease_ref=@lease_ref
 AND invocation.effect_fence_digest=@fence_digest AND invocation.claimed_fence_digest=@fence_digest
 AND invocation.workload_instance=@workload_instance AND invocation.claimed_workload='integration-gateway'
 AND NOT EXISTS (SELECT 1 FROM control_plane.integration_personal_invocation_admissions admission
   WHERE admission.invocation_id=invocation.id AND admission.organization_id=invocation.organization_id);
