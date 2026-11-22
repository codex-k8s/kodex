-- name: ssh_execution_test__dependency :one
SELECT invocation.state,coalesce(invocation.safe_error_code,''),gate.state,coalesce(gate.decision,''),root.state,node.state,
 (SELECT count(*) FROM control_plane.run_edges edge WHERE edge.root_run_id=root.id AND edge.source_node_id=node.id AND edge.type='CONTINUES')
FROM control_plane.integration_invocations invocation
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
JOIN control_plane.runs execution ON execution.id=invocation.run_id AND execution.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=execution.organization_id
JOIN control_plane.ssh_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.owner_gates gate ON gate.integration_invocation_id=invocation.id AND gate.organization_id=invocation.organization_id AND gate.kubernetes_child_id IS NULL
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref;
