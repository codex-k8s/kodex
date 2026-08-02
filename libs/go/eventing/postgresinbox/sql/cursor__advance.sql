-- name: cursor__advance :exec
UPDATE runtime_event_cursors
SET last_sequence = @event_sequence,
    last_event_id = @event_id::uuid,
    last_event_digest = @event_digest,
    updated_at = clock_timestamp()
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND ordering_key = @ordering_key::jsonb
  AND last_sequence + 1 = @event_sequence;
