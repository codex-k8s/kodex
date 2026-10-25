-- name: owner_gate_expiry_partial_trigger :exec
-- Только пустая disposable schema: UPDATE expires_at обходит column-specific trigger.
DROP TRIGGER owner_gate_expiry_immutable ON control_plane.owner_gates;
CREATE TRIGGER owner_gate_expiry_immutable BEFORE UPDATE OF state ON control_plane.owner_gates
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_owner_gate_expiry();
