-- name: cursor__ensure :exec
INSERT INTO runtime_event_cursors (
    consumer_name,
    consumer_scope,
    ordering_key
)
VALUES (
    @consumer_name,
    @consumer_scope,
    @ordering_key::jsonb
)
ON CONFLICT (consumer_name, consumer_scope, ordering_key) DO NOTHING;
