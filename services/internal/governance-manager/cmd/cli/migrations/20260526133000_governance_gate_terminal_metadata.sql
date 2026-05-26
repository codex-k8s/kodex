-- +goose Up
ALTER TABLE governance_manager_gate_requests
    ADD COLUMN terminal_actor_ref text NOT NULL DEFAULT '',
    ADD COLUMN terminal_reason text NOT NULL DEFAULT '',
    ADD COLUMN terminal_at timestamptz,
    ADD CONSTRAINT governance_manager_gate_requests_terminal_metadata_chk
        CHECK (
            (
                status IN ('cancelled', 'expired')
                AND terminal_actor_ref <> ''
                AND terminal_at IS NOT NULL
            )
            OR status NOT IN ('cancelled', 'expired')
        );

-- +goose Down
ALTER TABLE governance_manager_gate_requests
    DROP CONSTRAINT IF EXISTS governance_manager_gate_requests_terminal_metadata_chk,
    DROP COLUMN IF EXISTS terminal_at,
    DROP COLUMN IF EXISTS terminal_reason,
    DROP COLUMN IF EXISTS terminal_actor_ref;
