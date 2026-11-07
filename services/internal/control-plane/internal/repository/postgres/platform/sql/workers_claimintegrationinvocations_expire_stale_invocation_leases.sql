-- name: workers_claimintegrationinvocations_expire_stale_invocation_leases :exec
UPDATE control_plane.integration_invocations invocation
SET state=CASE WHEN risk='READ' AND NOT EXISTS (
      SELECT 1 FROM control_plane.integration_personal_invocation_admissions admission
      WHERE admission.invocation_id=invocation.id AND admission.organization_id=invocation.organization_id
        AND NOT admission.recovery_read_only
    ) THEN 'READY' ELSE 'UNKNOWN_OUTCOME' END,
    safe_error_code=CASE WHEN risk='READ' AND NOT EXISTS (
      SELECT 1 FROM control_plane.integration_personal_invocation_admissions admission
      WHERE admission.invocation_id=invocation.id AND admission.organization_id=invocation.organization_id
        AND NOT admission.recovery_read_only
    ) THEN safe_error_code ELSE 'INTEGRATION_OUTCOME_UNKNOWN' END,
    lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
    version=version+1,updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND claimed_workload=$2 AND state='RUNNING' AND lease_expires_at<=clock_timestamp()
