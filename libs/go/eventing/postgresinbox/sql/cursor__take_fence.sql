-- name: cursor__take_fence :one
UPDATE runtime_event_cursors
SET next_fence = next_fence + 1,
    updated_at = clock_timestamp()
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND ordering_key = @ordering_key::jsonb
  AND next_fence < 9223372036854775807
RETURNING next_fence - 1;
