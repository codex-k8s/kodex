-- name: workers_completeintegrationinvocation_select_integration_invocations_organization_id_ref :one
SELECT i.id::text,i.run_id::text,r.root_run_id::text,r.project_id::text,p.ref,n.ref,
	COALESCE(i.effect_fence_digest,''),i.generation,i.state,COALESCE(i.lease_ref,''),i.lease_expires_at,
	i.effect_key,i.input_digest,COALESCE(receipt.ref,''),COALESCE(receipt.effect_key,''),
	COALESCE(receipt.input_digest,''),COALESCE(receipt.provider_effect_ref,''),COALESCE(receipt.response_digest,''),
	COALESCE(receipt.result_summary,'')
FROM control_plane.integration_invocations i
JOIN control_plane.runs r ON r.id=i.run_id
JOIN control_plane.projects p ON p.id=r.project_id
JOIN control_plane.run_nodes n ON n.id=i.node_id
LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=i.effect_receipt_id
WHERE i.organization_id=$1::uuid AND i.ref=$2 AND i.claimed_workload=$3
FOR UPDATE OF i
