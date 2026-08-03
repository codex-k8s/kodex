-- name: inbox__recover_rejoin :exec
UPDATE runtime_inbox_events
SET state = 'RETRY',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    available_at = clock_timestamp(),
    updated_at = clock_timestamp(),
    last_error_code = 'broker_redelivery_exhausted'
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND event_id = @event_id::uuid
  AND event_digest = @event_digest
  AND lease_generation = @expected_generation
  AND lease_fence = @expected_fence
  AND attempts < max_attempts
  AND (
      state IN ('RECEIVED', 'RETRY')
      OR (state = 'PROCESSING' AND lease_expires_at <= clock_timestamp())
  );
