-- name: ContinuationRetry
UPDATE integration_gateway.continuation_effects SET
    lease_id = '', lease_expires_at = NULL,
    available_at = CASE WHEN continuation_id = '' THEN LEAST(
        clock_timestamp() + @backoff_milliseconds * interval '1 millisecond', application_grant_expires_at
    ) ELSE clock_timestamp() + @backoff_milliseconds * interval '1 millisecond' END,
    updated_at = clock_timestamp()
 WHERE invocation_id = @invocation_id
   AND action = @action AND lease_id = @lease_id AND lease_fence = @lease_fence
RETURNING invocation_id
