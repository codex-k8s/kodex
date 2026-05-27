-- name: delivery_attempt__list :many
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
    channel_capability_ref,
    package_installation_ref,
    package_version_ref,
    delivery_command_ref,
    callback_ref,
    callback_route_ref,
    runtime_ref,
    runtime_job_ref,
    routing_policy_ref,
    created_at,
    updated_at,
    sent_at
FROM interaction_hub_delivery_attempts
WHERE (@request_id::uuid IS NULL OR request_id = @request_id::uuid)
  AND (@notification_id::uuid IS NULL OR notification_id = @notification_id::uuid)
  AND (@delivery_id::text = '' OR delivery_id = @delivery_id)
ORDER BY attempt_number DESC, created_at DESC, id DESC
LIMIT @limit::integer;
