-- name: workers_getintegrationinvocation_select_integration_invocations_organization_id_ref :one
SELECT i.state,i.result_summary,i.safe_error_code,COALESCE(g.ref,''),COALESCE(receipt.ref,'')
FROM control_plane.integration_invocations i
LEFT JOIN control_plane.owner_gates g ON g.integration_invocation_id=i.id
LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=i.effect_receipt_id
WHERE i.organization_id=$1::uuid AND i.ref=$2
