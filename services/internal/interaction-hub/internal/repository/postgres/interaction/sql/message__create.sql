-- name: message__create :exec
INSERT INTO interaction_hub_messages (
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
) VALUES (
    @id,
    @thread_id,
    @message_kind,
    @author_ref,
    @body_summary,
    @body_object_uri,
    @body_object_digest,
    @body_object_size_bytes,
    @body_digest,
    @locale,
    @safe_metadata::jsonb,
    @created_at
);
