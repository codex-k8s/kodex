-- name: legacy_workflow_rework__read_join :many
SELECT invocation.id::text,invocation.state
FROM control_plane.run_nodes node
JOIN control_plane.integration_invocations invocation ON invocation.node_id=node.id
    AND invocation.organization_id=node.organization_id AND invocation.run_id=node.run_id AND invocation.risk='READ'
JOIN control_plane.owner_gates gate ON gate.integration_invocation_id=invocation.id
    AND gate.organization_id=node.organization_id AND gate.root_run_id=node.root_run_id
WHERE node.id=@node_id::uuid AND node.organization_id=@organization_id::uuid
    AND node.owner_rework_gate_id IS NOT NULL
ORDER BY invocation.id
LIMIT 2;
