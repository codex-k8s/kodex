-- +goose Up
ALTER TABLE control_plane.owner_gates
    ADD COLUMN expires_at timestamptz,
    DROP CONSTRAINT owner_gates_state_check,
    ADD CONSTRAINT owner_gates_state_check CHECK (
        state IN ('OPEN','APPROVED','REJECTED','CHANGES_REQUESTED','CANCELLED','EXPIRED')),
    DROP CONSTRAINT owner_gates_check,
    ADD CONSTRAINT owner_gates_resolution_check CHECK (
        (state='OPEN' AND decision IS NULL AND resolved_by IS NULL AND resolved_at IS NULL) OR
        (state='EXPIRED' AND decision IS NULL AND resolved_by IS NULL AND resolved_at IS NOT NULL
         AND expires_at IS NOT NULL AND integration_invocation_id IS NULL) OR
        (state IN ('APPROVED','REJECTED','CHANGES_REQUESTED','CANCELLED')
         AND decision IS NOT NULL AND resolved_by IS NOT NULL AND resolved_at IS NOT NULL)),
    ADD CONSTRAINT owner_gates_expiry_check CHECK (
        expires_at IS NULL OR (expires_at>created_at AND integration_invocation_id IS NULL));

CREATE INDEX owner_gates_due_expiry ON control_plane.owner_gates (organization_id,expires_at,id)
    WHERE state='OPEN' AND expires_at IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_owner_gate_expiry() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        RAISE EXCEPTION 'owner gate expiry is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER owner_gate_expiry_immutable BEFORE UPDATE ON control_plane.owner_gates
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_owner_gate_expiry();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'owner gate expiry migration is forward-only';
END $$;
-- +goose StatementEnd
