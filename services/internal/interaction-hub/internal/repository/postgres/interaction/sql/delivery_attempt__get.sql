-- name: delivery_attempt__get :one
SELECT
    id,
    request_id,
    notification_id,
    route_id,
    delivery_id,
    delivery_kind,
    status,
    channel_message_ref,
    attempt_number,
    next_retry_at,
    error_code,
    error_class,
    payload_digest,
    result_fingerprint,
    created_at,
    updated_at,
    sent_at
FROM interaction_hub_delivery_attempts
WHERE id = @id;
