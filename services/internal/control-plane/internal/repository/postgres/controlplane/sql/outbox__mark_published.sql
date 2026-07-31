UPDATE control_plane.outbox_events
SET
    published_at = clock_timestamp(),
    lease_owner = NULL,
    lease_token = NULL,
    lease_until = NULL,
    last_error_class = NULL
WHERE event_id = @event_id::uuid
  AND lease_token = @lease_token::uuid
  AND published_at IS NULL
