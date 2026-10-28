-- name: workflow_effect_proof :one
SELECT
    (SELECT count(*) FROM control_plane.integration_invocations invocation JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id WHERE node.root_run_id=root.id),
    (SELECT count(*) FROM control_plane.integration_effect_receipts receipt JOIN control_plane.integration_invocations invocation ON invocation.id=receipt.invocation_id AND invocation.organization_id=receipt.organization_id JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id WHERE node.root_run_id=root.id),
    (SELECT COALESCE(sum(jsonb_array_length(node.workflow_effect_intents)),0)::bigint FROM control_plane.run_nodes node WHERE node.root_run_id=root.id AND node.organization_id=root.organization_id),
    (SELECT COALESCE(sum(jsonb_array_length(node.workflow_effect_rejoins)),0)::bigint FROM control_plane.run_nodes node WHERE node.root_run_id=root.id AND node.organization_id=root.organization_id),
    (SELECT count(*) FROM control_plane.owner_gates gate WHERE gate.root_run_id=root.id AND gate.organization_id=root.organization_id),
    (SELECT count(*) FROM control_plane.integration_invocations invocation JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id WHERE node.root_run_id=root.id AND invocation.state IN ('READY','RUNNING','WAITING_APPROVAL','UNKNOWN_OUTCOME'))
FROM control_plane.runs root
WHERE root.ref=@run_ref AND root.organization_id=@organization_id::uuid AND root.id=root.root_run_id;
