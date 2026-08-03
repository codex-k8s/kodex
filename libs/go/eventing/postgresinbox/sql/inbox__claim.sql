-- name: inbox__claim :one
UPDATE runtime_inbox_events
SET state = 'PROCESSING',
    attempts = attempts + 1,
    lease_owner = @lease_owner,
    lease_token = @lease_token::uuid,
    lease_generation = lease_generation + 1,
    lease_fence = @lease_fence,
    lease_expires_at = clock_timestamp()
        + make_interval(secs => @lease_seconds),
    updated_at = clock_timestamp(),
    last_error_code = NULL
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND event_id = @event_id::uuid
  AND state IN ('RECEIVED', 'RETRY', 'PROCESSING')
  AND available_at <= clock_timestamp()
  AND (
      state <> 'PROCESSING'
      OR lease_expires_at <= clock_timestamp()
  )
  AND attempts < max_attempts
RETURNING
    lease_token::text,
    lease_generation,
    lease_fence,
    lease_expires_at,
    attempts,
    max_attempts;
