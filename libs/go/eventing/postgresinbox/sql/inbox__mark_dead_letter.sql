-- name: inbox__mark_dead_letter :exec
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
  AND state = 'PROCESSING'
  AND lease_owner = @lease_owner
  AND lease_token = @lease_token::uuid
  AND lease_generation = @lease_generation
  AND lease_fence = @lease_fence
  AND lease_expires_at > clock_timestamp();
