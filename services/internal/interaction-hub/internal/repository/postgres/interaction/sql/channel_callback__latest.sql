-- name: channel_callback__latest :one
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
WHERE (
    cardinality(@delivery_attempt_ids::uuid[]) > 0
    AND delivery_attempt_id = ANY(@delivery_attempt_ids::uuid[])
)
   OR (@request_id::uuid IS NOT NULL AND request_id = @request_id::uuid)
   OR (@delivery_id::text <> '' AND delivery_id = @delivery_id)
ORDER BY created_at DESC, id DESC
LIMIT 1;
