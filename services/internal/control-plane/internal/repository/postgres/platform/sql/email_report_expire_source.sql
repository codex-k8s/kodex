-- name: email_report_expire_source :exec
UPDATE control_plane.integration_invocations i
SET state='UNKNOWN_OUTCOME',safe_error_code='INTEGRATION_OUTCOME_UNKNOWN',
    lease_ref=NULL,effect_fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,
    version=i.version+1,updated_at=clock_timestamp()
FROM control_plane.email_authorizations a
WHERE a.organization_id=$1::uuid AND a.ref=$2 AND i.id=a.invocation_id AND i.organization_id=a.organization_id
  AND i.claimed_workload='integration-gateway' AND i.claimed_lease_ref=a.lease_ref
  AND i.claimed_fence_digest=a.fence_digest AND i.generation=a.generation
  AND i.risk<>'READ' AND i.state='RUNNING' AND i.lease_expires_at<=clock_timestamp();
