-- name: inbox__requeue :exec
UPDATE runtime_inbox_events
SET state = 'RETRY',
    attempts = 0,
    repair_count = repair_count + 1,
    available_at = clock_timestamp(),
    updated_at = clock_timestamp(),
    terminal_at = NULL,
    last_error_code = 'repaired'
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND event_id = @event_id::uuid
  AND event_digest = @event_digest
  AND event_sequence = @event_sequence
  AND state = 'DEAD_LETTER'
  AND lease_generation = @expected_generation
  AND lease_fence = @expected_fence
  AND repair_count < max_repairs;
