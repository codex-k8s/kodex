-- name: workflow_evidence__read_outcomes :many
SELECT fact.ref,fact.operation,fact.state,fact.result_summary,fact.safe_error_code,
 COALESCE(receipt.ref,''),fact.input_digest,COALESCE(gate.ref,''),COALESCE(gate.version,0),
 COALESCE(gate.decision,''),COALESCE(gate.decision_comment,'')
FROM control_plane.integration_invocations fact
JOIN control_plane.run_nodes source ON source.id=fact.node_id AND source.organization_id=fact.organization_id
LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.organization_id=fact.organization_id
 AND receipt.id=fact.effect_receipt_id AND receipt.invocation_id=fact.id
 AND receipt.effect_key=fact.effect_key AND receipt.input_digest=fact.input_digest
LEFT JOIN control_plane.owner_gates gate ON gate.organization_id=fact.organization_id
 AND gate.integration_invocation_id=fact.id AND gate.root_run_id=source.root_run_id
WHERE fact.organization_id=@organization_id::uuid AND fact.node_id=@source_node_id::uuid
ORDER BY fact.ref;
