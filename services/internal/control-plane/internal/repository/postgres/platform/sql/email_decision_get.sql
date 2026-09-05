-- name: EmailDecisionGet :one
SELECT d.ref,e.ref,d.receipt_version,d.receipt_digest,i.ref,d.outcome,d.grant_ref,s.ref,d.created_at,d.expires_at
FROM control_plane.email_reconciliation_decisions d
JOIN control_plane.email_effect_receipts e ON e.id=d.receipt_id AND e.organization_id=d.organization_id
JOIN control_plane.integration_invocations i ON i.id=e.invocation_id
JOIN control_plane.subjects s ON s.id=d.actor_id AND s.organization_id=d.organization_id
WHERE d.organization_id=@organization_id::uuid AND e.ref=@receipt_ref
  AND (@decision_ref::text='' OR d.ref=@decision_ref)
ORDER BY d.created_at DESC,d.ref DESC LIMIT 1;
