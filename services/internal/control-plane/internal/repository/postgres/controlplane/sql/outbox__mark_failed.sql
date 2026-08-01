UPDATE control_plane.outbox_events
SET
    attempts = attempts + 1,
    available_at = @available_at,
    terminal = (NOT @retryable OR attempts + 1 >= @max_attempts),
    last_error_class = 'publish_failed',
    lease_owner = NULL,
    lease_token = NULL,
    lease_until = NULL
WHERE event_id = @event_id::uuid
  AND lease_token = @lease_token::uuid
  AND published_at IS NULL
