-- name: ssh_runtime__result :one
SELECT lease.expires_at,clock_timestamp(),lease.run_id::text,invocation.ref,invocation.state,
 CASE WHEN invocation.state='SUCCEEDED' THEN invocation.result_summary ELSE '' END,
 coalesce(invocation.safe_error_code,''),
 CASE WHEN invocation.state='SUCCEEDED' THEN coalesce(receipt.ref,'') ELSE '' END,
 CASE WHEN invocation.state='SUCCEEDED' THEN coalesce(receipt.response_digest,'') ELSE '' END,
 EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.organization_id=invocation.organization_id
  AND gate.integration_invocation_id=invocation.id AND gate.root_run_id=pin.root_run_id
  AND gate.state='OPEN' AND gate.kubernetes_child_id IS NULL)
FROM control_plane.integration_invocations invocation
JOIN control_plane.runtime_leases lease ON lease.node_id=invocation.node_id AND lease.run_id=invocation.run_id AND lease.organization_id=invocation.organization_id
JOIN control_plane.ssh_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id
 AND receipt.invocation_id=invocation.id AND receipt.organization_id=invocation.organization_id
 AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
 AND receipt.result_summary=invocation.result_summary
WHERE invocation.organization_id=@organization_id::uuid AND invocation.id=@invocation_id::uuid
 AND invocation.resource_kind='SSH_TARGET'
 AND lease.ref=@lease_ref AND lease.fence_digest=@fence_digest AND lease.generation=@generation
 AND lease.input_digest=@runtime_input_digest AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
 AND control_plane.runtime_execution_before_deadline(lease.organization_id,lease.run_id);
