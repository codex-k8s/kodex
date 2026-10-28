-- name: repository_bundle__clear_invocation_lease :exec
UPDATE control_plane.integration_invocations
SET state='UNKNOWN_OUTCOME',safe_error_code='INTEGRATION_OUTCOME_UNKNOWN',
    lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
    version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@invocation_id::uuid
  AND operation='github.repository.bundle.publish' AND state='RUNNING'
  AND lease_ref=@lease_ref AND effect_fence_digest=@fence_digest AND generation=@generation;
