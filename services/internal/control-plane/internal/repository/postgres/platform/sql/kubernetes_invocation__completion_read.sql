-- name: kubernetes_invocation__completion_read :one
SELECT invocation.id::text,invocation.state,invocation.result_summary,
 coalesce(plan.ref,''),coalesce(plan.plan_digest,''),coalesce(plan.snapshot_digest,''),
 coalesce(receipt.ref,''),coalesce(receipt.response_digest,''),coalesce(receipt.result_summary,''),
 invocation.run_id::text,root.root_run_id::text,coalesce(root.project_id::text,''),node.ref,
 invocation.effect_key,invocation.lease_expires_at,clock_timestamp(),
 EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.integration_invocation_id=invocation.id
  AND gate.organization_id=invocation.organization_id AND gate.state='OPEN')
FROM control_plane.integration_invocations invocation
JOIN control_plane.runs root ON root.id=invocation.run_id AND root.organization_id=invocation.organization_id
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
LEFT JOIN control_plane.kubernetes_execution_plans plan ON plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id
 AND receipt.organization_id=invocation.organization_id AND receipt.invocation_id=invocation.id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.resource_kind='KUBERNETES_TARGET' AND invocation.input_digest=@input_digest
 AND invocation.generation=@generation AND invocation.claimed_workload='integration-gateway'
 AND invocation.claimed_lease_ref=@lease_ref AND invocation.claimed_fence_digest=@fence_digest
 AND invocation.kubernetes_claimed_instance=@instance
 AND(plan.id IS NULL OR plan.binding_digest=@binding_digest);
