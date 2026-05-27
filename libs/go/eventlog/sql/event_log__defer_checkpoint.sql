-- name: event_log__defer_checkpoint :exec
UPDATE platform_event_consumer_checkpoints
SET
    locked_until = @locked_until,
    retry_sequence_id = @retry_sequence_id,
    retry_attempt = @retry_attempt,
    last_error = @last_error,
    updated_at = @updated_at
WHERE consumer_name = @consumer_name
  AND lease_owner = @lease_owner
  AND locked_until > @now;
