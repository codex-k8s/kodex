-- name: gate_intent_source_turn :one
SELECT predecessor.turn_id::text
FROM control_plane.owner_gates gate
JOIN control_plane.run_nodes node ON node.id=gate.node_id
JOIN control_plane.run_nodes predecessor ON predecessor.id=node.parent_node_id
WHERE gate.organization_id=$1::uuid AND gate.ref=$2;
