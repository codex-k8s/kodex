-- name: owner_gate_expiry_wrong_grouping :exec
-- Только пустая disposable schema: несовместимая группировка должна быть отвергнута readback.
ALTER TABLE control_plane.owner_gates DROP CONSTRAINT owner_gates_expiry_check;
ALTER TABLE control_plane.owner_gates ADD CONSTRAINT owner_gates_expiry_check
    CHECK ((expires_at IS NULL OR expires_at>created_at) AND integration_invocation_id IS NULL);
