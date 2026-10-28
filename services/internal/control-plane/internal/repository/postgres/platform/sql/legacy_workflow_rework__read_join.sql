-- name: legacy_workflow_rework__read_join :one
SELECT COALESCE(min(invocation.id::text),''),count(*) FILTER (WHERE invocation.state NOT IN ('SUCCEEDED','FAILED','REJECTED','CANCELLED'))
FROM control_plane.run_nodes node
JOIN control_plane.integration_invocations invocation ON invocation.node_id=node.id
    AND invocation.organization_id=node.organization_id AND invocation.run_id=node.run_id AND invocation.risk='READ'
WHERE node.id=@node_id::uuid AND node.organization_id=@organization_id::uuid
    AND node.owner_rework_gate_id IS NOT NULL;
