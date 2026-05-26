-- name: message__list :many
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
WHERE thread_id = @thread_id
ORDER BY created_at, id
LIMIT @limit::integer
OFFSET @offset::bigint;
