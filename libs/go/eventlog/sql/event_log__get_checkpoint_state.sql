-- name: event_log__get_checkpoint_state :one
SELECT
    consumer_name,
    last_sequence_id,
    lease_owner,
    locked_until,
    updated_at
FROM platform_event_consumer_checkpoints
WHERE consumer_name = @consumer_name;
