-- +goose Up
SET ROLE control_plane_owner;

-- Контракт assistant turn допускает 32768 Unicode codepoints; исходный текст не усекать.
ALTER TABLE control_plane.runs DROP CONSTRAINT runs_task_check;
ALTER TABLE control_plane.runs ADD CONSTRAINT runs_task_check
    CHECK (char_length(task) BETWEEN 1 AND 32768);

-- USER-текст остаётся exact: JSON-escaping 32768 codepoints плюс прежние bounded metadata.
ALTER TABLE control_plane.run_events DROP CONSTRAINT run_events_safe_delta_check;
ALTER TABLE control_plane.run_events ADD CONSTRAINT run_events_safe_delta_check
    CHECK (jsonb_typeof(safe_delta) = 'object' AND octet_length(safe_delta::text) <= 262144);

-- 4 KiB остаются broker headers внутри source-pinned stream256KiB.
ALTER TABLE control_plane.outbox_events DROP CONSTRAINT outbox_events_payload_check;
ALTER TABLE control_plane.outbox_events ADD CONSTRAINT outbox_events_payload_check
    CHECK (octet_length(payload) <= 258048);

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'assistant turn input bound is forward-only';
END $$;
-- +goose StatementEnd
