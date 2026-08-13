-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;

-- +goose StatementBegin
DO $$
DECLARE
    stored_version bigint;
BEGIN
    SELECT version
      INTO stored_version
      FROM control_plane.schema_state
     WHERE singleton = true
     FOR UPDATE;

    IF stored_version NOT IN (20260809026310, 20260812000100) THEN
        RAISE EXCEPTION 'control-plane schema fence repair source version is invalid'
            USING ERRCODE = '55000';
    END IF;

    UPDATE control_plane.schema_state
       SET version = 20260813000100,
           migrated_at = clock_timestamp()
     WHERE singleton = true;
END;
$$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- Forward-only: runtime may already have admitted the repaired schema fence.
SELECT 1;
