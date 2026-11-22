-- name: email_report__admitted_source :one
SELECT i.state
FROM control_plane.email_authorizations a
JOIN control_plane.integration_invocations i ON i.id=a.invocation_id AND i.organization_id=a.organization_id
JOIN control_plane.email_effect_receipts receipt ON receipt.id=@receipt_id::uuid
  AND receipt.organization_id=a.organization_id AND receipt.invocation_id=i.id AND receipt.authorization_ref=a.ref
WHERE a.organization_id=@organization_id::uuid AND a.ref=@authorization_ref
  AND i.claimed_workload='integration-gateway' AND i.claimed_lease_ref=a.lease_ref
  AND i.claimed_fence_digest=a.fence_digest AND i.generation=a.generation
  AND i.risk<>'READ' AND i.state IN ('RUNNING','UNKNOWN_OUTCOME','SUCCEEDED','FAILED','CANCELLED')
FOR UPDATE OF i;
