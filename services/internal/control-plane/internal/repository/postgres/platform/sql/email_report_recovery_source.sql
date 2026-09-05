-- name: email_report_recovery_source :one
SELECT i.state
FROM control_plane.email_authorizations a
JOIN control_plane.integration_invocations i ON i.id=a.invocation_id AND i.organization_id=a.organization_id
WHERE a.organization_id=$1::uuid AND a.ref=$2
  AND i.claimed_workload='integration-gateway' AND i.claimed_lease_ref=a.lease_ref
  AND i.claimed_fence_digest=a.fence_digest AND i.generation=a.generation
  AND i.risk<>'READ' AND i.state IN ('RUNNING','UNKNOWN_OUTCOME','SUCCEEDED','FAILED','CANCELLED')
  AND (i.state<>'RUNNING' OR i.lease_expires_at<=clock_timestamp())
FOR UPDATE OF i;
