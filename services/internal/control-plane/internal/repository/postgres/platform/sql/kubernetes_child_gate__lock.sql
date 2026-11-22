-- name: kubernetes_child_gate__lock :one
SELECT gate.id::text FROM control_plane.owner_gates gate
WHERE gate.organization_id=@organization_id::uuid AND gate.ref=@gate_ref AND gate.kubernetes_child_id IS NOT NULL
FOR UPDATE;
