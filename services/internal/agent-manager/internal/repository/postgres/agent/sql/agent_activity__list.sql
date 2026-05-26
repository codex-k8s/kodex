-- name: agent_activity__list :many
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
WHERE (@session_id::uuid IS NULL OR session_id = @session_id::uuid)
  AND (@run_id::uuid IS NULL OR run_id = @run_id::uuid)
  AND (@activity_kind::text IS NULL OR activity_kind = @activity_kind::text)
  AND (@activity_status::text IS NULL OR status = @activity_status::text)
ORDER BY started_at DESC, id DESC
LIMIT @limit::int
OFFSET @offset::int;
