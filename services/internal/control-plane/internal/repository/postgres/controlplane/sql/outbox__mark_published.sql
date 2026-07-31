DELETE FROM control_plane.outbox_events
WHERE event_id = @event_id::uuid
  AND lease_token = @lease_token::uuid
  AND published_at IS NULL
