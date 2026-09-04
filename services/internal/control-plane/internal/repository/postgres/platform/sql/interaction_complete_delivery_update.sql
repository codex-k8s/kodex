-- name: interaction_complete_delivery_update :one
UPDATE control_plane.interaction_deliveries
SET state = CASE WHEN @success THEN 'SUCCEEDED' WHEN @confirmed_no_effect THEN 'FAILED' ELSE 'UNKNOWN_OUTCOME' END,
    external_post_ref = CASE WHEN @success THEN @external_post_ref ELSE external_post_ref END,
    external_thread_ref = CASE WHEN @success THEN NULLIF(@external_thread_ref, '') ELSE external_thread_ref END,
    safe_error_code = CASE WHEN @success THEN '' WHEN @confirmed_no_effect THEN @safe_error_code ELSE 'INTERACTION_OUTCOME_UNKNOWN' END,
    available_at = CASE WHEN @confirmed_no_effect THEN clock_timestamp() + make_interval(secs => LEAST(300, 15 * @attempt)) ELSE available_at END,
    lease_ref = NULL,
    fence_digest = NULL,
    workload_instance = NULL,
    lease_expires_at = NULL,
    completed_at = CASE WHEN @success THEN clock_timestamp() ELSE NULL END,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE id = @delivery_id::uuid
RETURNING ref, state
