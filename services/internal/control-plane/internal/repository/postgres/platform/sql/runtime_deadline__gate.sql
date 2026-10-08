-- name: runtime_deadline__gate :one
SELECT node.run_id::text FROM control_plane.owner_gates gate
JOIN control_plane.run_nodes node ON node.id=gate.node_id AND node.organization_id=gate.organization_id
WHERE gate.organization_id=@organization_id::uuid AND gate.ref=@gate_ref;
