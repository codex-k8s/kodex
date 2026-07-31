WITH candidates AS (
    SELECT event_id
    FROM control_plane.outbox_events
    WHERE published_at IS NULL
      AND terminal = false
      AND available_at <= clock_timestamp()
      AND (lease_until IS NULL OR lease_until <= clock_timestamp())
    ORDER BY available_at, occurred_at, event_id
    FOR UPDATE SKIP LOCKED
    LIMIT @limit
)
UPDATE control_plane.outbox_events AS event
SET
    lease_owner = @lease_owner,
    lease_token = gen_random_uuid(),
    lease_until = clock_timestamp() + @lease_duration::interval
FROM candidates
WHERE event.event_id = candidates.event_id
RETURNING event.envelope, event.lease_token::text, event.attempts
