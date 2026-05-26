-- name: message__get :one
SELECT
    id,
    thread_id,
    message_kind,
    author_ref,
    body_summary,
    body_object_uri,
    body_object_digest,
    body_object_size_bytes,
    body_digest,
    locale,
    safe_metadata,
    created_at
FROM interaction_hub_messages
WHERE id = @id;
