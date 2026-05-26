-- name: channel_callback__get :one
SELECT
    id,
    callback_id,
    delivery_id,
    delivery_attempt_id,
    request_id,
    source_route_id,
    actor_ref,
    action,
    callback_summary,
    callback_object_uri,
    callback_object_digest,
    callback_object_size_bytes,
    signature_status,
    processing_status,
    error_code,
    received_at,
    created_at,
    callback_route_ref,
    gateway_ref,
    correlation_id,
    callback_fingerprint
FROM interaction_hub_channel_callbacks
WHERE id = @id;
