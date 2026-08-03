-- name: inbox__mark_retry :exec
UPDATE runtime_inbox_events
SET state = 'RETRY',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    available_at = clock_timestamp()
        + make_interval(secs => @backoff_seconds),
    updated_at = clock_timestamp(),
    last_error_code = @error_code
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND event_id = @event_id::uuid
  AND event_digest = @event_digest
  AND state = 'PROCESSING'
  AND lease_owner = @lease_owner
  AND lease_token = @lease_token::uuid
  AND lease_generation = @lease_generation
  AND lease_fence = @lease_fence
  AND lease_expires_at > clock_timestamp();
