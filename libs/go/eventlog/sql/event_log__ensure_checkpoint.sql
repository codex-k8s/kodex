-- name: event_log__ensure_checkpoint :exec
INSERT INTO platform_event_consumer_checkpoints (
    consumer_name,
    last_sequence_id,
    lease_owner,
    locked_until,
    updated_at
)
VALUES (
    @consumer_name,
    0,
    '',
    NULL,
    @updated_at
)
ON CONFLICT (consumer_name) DO NOTHING;
