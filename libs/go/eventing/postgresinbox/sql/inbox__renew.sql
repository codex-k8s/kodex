-- name: inbox__renew :one
UPDATE runtime_inbox_events
SET lease_expires_at = clock_timestamp()
        + make_interval(secs => @lease_seconds),
    updated_at = clock_timestamp()
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND event_id = @event_id::uuid
  AND event_digest = @event_digest
  AND state = 'PROCESSING'
  AND lease_owner = @lease_owner
  AND lease_token = @lease_token::uuid
  AND lease_generation = @lease_generation
  AND lease_fence = @lease_fence
  AND lease_expires_at > clock_timestamp()
RETURNING lease_expires_at;
