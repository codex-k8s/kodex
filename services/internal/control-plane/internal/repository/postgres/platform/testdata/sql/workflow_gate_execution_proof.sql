-- name: workflow_gate_execution_proof :one
SELECT root.state,physical.state,node.state,lease.state,
    COALESCE(root.execution_started_at,root.created_at),COALESCE(physical.execution_started_at,physical.created_at),
    (SELECT count(*) FROM control_plane.owner_gates gate JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id AND gate_node.organization_id=gate.organization_id WHERE gate.organization_id=root.organization_id AND gate.root_run_id=root.id AND gate.state='OPEN' AND gate_node.run_id=physical.id),
    (SELECT count(*) FROM control_plane.owner_gates gate WHERE gate.organization_id=root.organization_id AND gate.root_run_id=root.id AND gate.state='OPEN'),
    (SELECT count(*) FROM control_plane.integration_invocations invocation JOIN control_plane.run_nodes producer ON producer.id=invocation.node_id AND producer.organization_id=invocation.organization_id WHERE producer.root_run_id=root.id),
    (SELECT count(*) FROM control_plane.integration_effect_receipts receipt JOIN control_plane.integration_invocations invocation ON invocation.id=receipt.invocation_id AND invocation.organization_id=receipt.organization_id JOIN control_plane.run_nodes producer ON producer.id=invocation.node_id AND producer.organization_id=invocation.organization_id WHERE producer.root_run_id=root.id),
    (SELECT count(*) FROM control_plane.runtime_leases active JOIN control_plane.run_nodes model ON model.id=active.node_id AND model.organization_id=active.organization_id WHERE model.root_run_id=root.id AND active.state='CLAIMED'),
    COALESCE(root.execution_deadline_at::text,'NONE'),COALESCE(physical.execution_deadline_at::text,'NONE')
FROM control_plane.runtime_leases lease
JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.organization_id=lease.organization_id
JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=node.organization_id
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
WHERE node.ref=@node_ref AND lease.ref=@lease_ref;
