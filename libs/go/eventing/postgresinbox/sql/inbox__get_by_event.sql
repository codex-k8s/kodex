-- name: inbox__get_by_event :one
SELECT
    event_id::text,
    event_digest,
    ordering_key::text,
    event_sequence,
    state,
    attempts,
    max_attempts,
    repair_count,
    max_repairs,
    lease_owner,
    lease_token::text,
    lease_generation,
    lease_fence,
    lease_expires_at,
    available_at,
    last_error_code,
    processed_at,
    cleanup_after,
    terminal_at,
    available_at <= clock_timestamp() AS available_now,
    state = 'PROCESSING'
        AND lease_expires_at > clock_timestamp() AS lease_active
FROM runtime_inbox_events
WHERE consumer_name = @consumer_name
  AND consumer_scope = @consumer_scope
  AND event_id = @event_id::uuid;
