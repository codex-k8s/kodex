-- name: event_log__claim :many
WITH checkpoint AS (
    SELECT consumer_name, last_sequence_id
    FROM platform_event_consumer_checkpoints
    WHERE consumer_name = @consumer_name
      AND (locked_until IS NULL OR locked_until <= @now)
    FOR UPDATE
),
events AS (
    SELECT
        sequence_id,
        event_id,
        source_service,
        event_type,
        schema_version,
        aggregate_type,
        aggregate_id,
        payload,
        occurred_at,
        recorded_at
    FROM platform_event_log
    WHERE sequence_id > COALESCE((SELECT last_sequence_id FROM checkpoint), 0)
    ORDER BY sequence_id
    LIMIT @limit
),
lease AS (
    UPDATE platform_event_consumer_checkpoints
    SET
        lease_owner = @lease_owner,
        locked_until = @locked_until,
        updated_at = @now
    WHERE consumer_name = @consumer_name
      AND EXISTS (SELECT 1 FROM checkpoint)
      AND EXISTS (SELECT 1 FROM events)
    RETURNING consumer_name
)
SELECT
    sequence_id,
    event_id,
    source_service,
    event_type,
    schema_version,
    aggregate_type,
    aggregate_id,
    payload,
    occurred_at,
    recorded_at
FROM events
WHERE EXISTS (SELECT 1 FROM lease)
ORDER BY sequence_id;
