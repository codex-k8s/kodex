-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;

-- Legacy-материализация до commit проверяет неизменяемый event envelope.
-- Существующий metadata-grant не включает эти поля доказательства, поэтому
-- доступ остается столбцовым и ограничивается действующей tenant/project RLS.
GRANT SELECT (
    aggregate_type,
    aggregate_version,
    correlation_id,
    causation_id,
    available_at,
    envelope
) ON control_plane.outbox_events TO control_plane_runtime;

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

    IF stored_version IS DISTINCT FROM 20260817000200 THEN
        RAISE EXCEPTION 'legacy evidence outbox read source version is invalid'
            USING ERRCODE = '55000';
    END IF;

    UPDATE control_plane.schema_state
       SET version = 20260820000100,
           migrated_at = clock_timestamp()
     WHERE singleton = true;
END;
$$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- Forward-only: чтение evidence обязательно для проверки migration receipts.
SELECT 1;
