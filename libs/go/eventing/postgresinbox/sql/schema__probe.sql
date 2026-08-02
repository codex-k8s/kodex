-- name: schema__probe :one
SELECT
    runtime_event_ordering_key(NULL, 'event', 'aggregate', 'id') =
        '["event", "aggregate", "id"]'::jsonb
    AND NOT EXISTS (
        SELECT 1 FROM runtime_event_schema_versions WHERE false
    )
    AND NOT EXISTS (
        SELECT 1 FROM runtime_event_cursors WHERE false
    )
    AND NOT EXISTS (
        SELECT 1 FROM runtime_inbox_events WHERE false
    )
    AND NOT EXISTS (
        SELECT 1 FROM runtime_inbox_repairs WHERE false
    ) AS working_path_ready;
