-- name: thread__get :one
SELECT
    id,
    scope_type,
    scope_ref,
    thread_kind,
    primary_actor_ref,
    source_kind,
    source_ref,
    status,
    latest_message_id,
    correlation_id,
    retention_class,
    version,
    created_at,
    updated_at,
    closed_at
FROM interaction_hub_threads
WHERE id = @id;
