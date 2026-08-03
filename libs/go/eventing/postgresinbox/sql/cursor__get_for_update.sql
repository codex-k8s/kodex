-- name: cursor__get_for_update :one
SELECT
    last_sequence,
    last_event_id::text,
    last_event_digest,
    next_fence
FROM runtime_event_cursors
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND ordering_key = @ordering_key::jsonb
FOR UPDATE;
