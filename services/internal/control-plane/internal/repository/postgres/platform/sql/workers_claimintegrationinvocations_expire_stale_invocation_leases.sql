-- name: workers_claimintegrationinvocations_expire_stale_invocation_leases :exec
UPDATE control_plane.integration_invocations
SET state=CASE WHEN risk='READ' THEN 'READY' ELSE 'UNKNOWN_OUTCOME' END,
    safe_error_code=CASE WHEN risk='READ' THEN safe_error_code ELSE 'INTEGRATION_OUTCOME_UNKNOWN' END,
    lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
    version=version+1,updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND claimed_workload=$2 AND state='RUNNING' AND lease_expires_at<=clock_timestamp()
