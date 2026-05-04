-- name: event_log__advance_checkpoint :exec
UPDATE platform_event_consumer_checkpoints
SET
    last_sequence_id = GREATEST(last_sequence_id, @last_sequence_id),
    lease_owner = '',
    locked_until = NULL,
    updated_at = @updated_at
WHERE consumer_name = @consumer_name
  AND lease_owner = @lease_owner
  AND locked_until > @now;
