-- name: inbox__complete :exec
UPDATE runtime_inbox_events
SET state = 'COMPLETED',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = clock_timestamp(),
    cleanup_after = clock_timestamp()
        + make_interval(secs => @retention_seconds),
    updated_at = clock_timestamp(),
    last_error_code = NULL
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
