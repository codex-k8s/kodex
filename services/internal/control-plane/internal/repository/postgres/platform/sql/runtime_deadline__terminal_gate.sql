-- name: runtime_deadline__terminal_gate :one
SELECT gate.state
FROM control_plane.owner_gates gate
WHERE gate.organization_id=@organization_id::uuid AND gate.ref=@gate_ref AND gate.state<>'OPEN'
FOR UPDATE OF gate;
