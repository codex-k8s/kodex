-- name: OutboxTerminalList
SELECT
    event_id::text, ordering_key, event_sequence, event_name,
    aggregate_id::text, attempts, repair_count,
    coalesce(last_error_class, ''), occurred_at, updated_at
FROM control_plane.outbox_events
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND terminal = true
  AND published_at IS NULL
  AND (@after_event_id = '' OR event_id > @after_event_id::uuid)
ORDER BY event_id
LIMIT @limit
