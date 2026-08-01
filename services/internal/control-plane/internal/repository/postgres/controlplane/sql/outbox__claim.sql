WITH cleanup AS (
    DELETE FROM control_plane.outbox_events
    WHERE published_at IS NOT NULL
      AND cleanup_after <= clock_timestamp()
),
candidates AS (
    SELECT event_id
    FROM control_plane.outbox_events AS candidate
    WHERE candidate.published_at IS NULL
      AND candidate.terminal = false
      AND candidate.available_at <= clock_timestamp()
      AND (candidate.lease_until IS NULL OR candidate.lease_until <= clock_timestamp())
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.outbox_events AS predecessor
          WHERE predecessor.ordering_key = candidate.ordering_key
            AND predecessor.event_sequence < candidate.event_sequence
            AND predecessor.published_at IS NULL
      )
    ORDER BY candidate.available_at, candidate.occurred_at, candidate.event_id
    FOR UPDATE OF candidate SKIP LOCKED
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
