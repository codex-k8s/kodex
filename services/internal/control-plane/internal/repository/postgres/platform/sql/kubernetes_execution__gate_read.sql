-- name: kubernetes_execution__gate_read :one
SELECT gate.ref,gate.state FROM control_plane.owner_gates gate
WHERE gate.organization_id=@organization_id::uuid AND gate.kubernetes_child_id=@child_id::uuid;
