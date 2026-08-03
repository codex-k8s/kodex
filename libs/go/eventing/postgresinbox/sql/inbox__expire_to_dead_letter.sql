-- name: inbox__expire_to_dead_letter :exec
UPDATE runtime_inbox_events
SET state = 'DEAD_LETTER',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    terminal_at = clock_timestamp(),
    updated_at = clock_timestamp(),
    last_error_code = @error_code
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND event_id = @event_id::uuid
  AND event_digest = @event_digest
  AND state IN ('RECEIVED', 'RETRY', 'PROCESSING')
  AND attempts >= max_attempts
  AND (
      state <> 'PROCESSING'
      OR lease_expires_at <= clock_timestamp()
  );
