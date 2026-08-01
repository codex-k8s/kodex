-- name: OutboxMarkPublished
UPDATE control_plane.outbox_events
SET
    published_at = @published_at,
    broker_stream = @broker_stream,
    broker_sequence = @broker_sequence,
    broker_duplicate = @broker_duplicate,
    delivery_receipt_at = @published_at,
    cleanup_after = @cleanup_after,
    lease_owner = NULL,
    lease_token = NULL,
    lease_until = NULL,
    last_error_class = NULL
WHERE event_id = @event_id::uuid
  AND lease_token = @lease_token::uuid
  AND published_at IS NULL
