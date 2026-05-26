-- name: delivery_attempt__update :exec
UPDATE interaction_hub_delivery_attempts
SET status = @status,
    channel_message_ref = @channel_message_ref,
    next_retry_at = @next_retry_at::timestamptz,
    error_code = @error_code,
    error_class = @error_class,
    result_fingerprint = @result_fingerprint,
    runtime_ref = @runtime_ref,
    runtime_job_ref = @runtime_job_ref,
    updated_at = @updated_at,
    sent_at = @sent_at::timestamptz
WHERE id = @id
  AND status NOT IN ('delivered', 'failed', 'cancelled', 'expired')
  AND result_fingerprint = '';
