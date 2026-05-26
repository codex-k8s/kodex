-- name: agent_activity__get :one
SELECT
    id,
    session_id,
    run_id,
    turn_id,
    tool_use_id,
    activity_kind,
    tool_name,
    tool_category,
    status,
    started_at,
    finished_at,
    duration_ms,
    safe_summary,
    payload_digest,
    bounded_error,
    safe_refs_json,
    safe_details_json,
    correlation_id,
    idempotency_key,
    version,
    created_at,
    updated_at
FROM agent_manager_agent_activities
WHERE id = @id;
