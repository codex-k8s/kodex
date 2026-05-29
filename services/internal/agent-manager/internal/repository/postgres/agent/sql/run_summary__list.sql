-- name: run_summary__list :many
WITH run_items AS (
    SELECT
        r.id,
        r.session_id,
        r.flow_version_id,
        r.stage_id,
        r.role_profile_id,
        r.role_profile_version,
        r.role_profile_digest,
        r.prompt_template_version_id,
        r.prompt_template_digest,
        r.runtime_context,
        r.provider_target,
        r.guidance_refs,
        r.status,
        r.result_summary,
        r.failure_code,
        r.version,
        r.started_at,
        r.finished_at,
        r.created_at,
        r.updated_at,
        gate.id AS human_gate_request_ref,
        gate.reason_code AS human_gate_reason_code,
        follow_up.id AS follow_up_ref,
        activity.id AS latest_activity_id,
        activity.activity_kind AS latest_activity_kind,
        activity.status AS latest_activity_status,
        activity.tool_name AS latest_activity_tool_name,
        activity.tool_category AS latest_activity_tool_category,
        activity.safe_summary AS latest_activity_safe_summary,
        activity.payload_digest AS latest_activity_payload_digest,
        activity.bounded_error AS latest_activity_bounded_error,
        activity.started_at AS latest_activity_started_at,
        activity.finished_at AS latest_activity_finished_at,
        activity.version AS latest_activity_version,
        activity.updated_at AS latest_activity_updated_at,
        CASE
            WHEN gate.id IS NOT NULL THEN 0
            WHEN r.status IN ('requested', 'starting', 'running', 'waiting') THEN 1
            ELSE 2
        END AS sort_bucket,
        GREATEST(r.updated_at, COALESCE(activity.started_at, r.updated_at)) AS sort_time
    FROM agent_manager_runs r
    JOIN agent_manager_sessions s ON s.id = r.session_id
    LEFT JOIN LATERAL (
        SELECT id, reason_code
        FROM agent_manager_human_gate_requests
        WHERE session_id = r.session_id
          AND run_id = r.id
          AND status = 'waiting'
        ORDER BY updated_at DESC, id DESC
        LIMIT 1
    ) gate ON TRUE
    LEFT JOIN LATERAL (
        SELECT id
        FROM agent_manager_follow_up_intents
        WHERE run_id = r.id
          AND status IN ('planned', 'requested')
        ORDER BY updated_at DESC, id DESC
        LIMIT 1
    ) follow_up ON TRUE
    LEFT JOIN LATERAL (
        SELECT
            id,
            activity_kind,
            status,
            tool_name,
            tool_category,
            safe_summary,
            payload_digest,
            bounded_error,
            started_at,
            finished_at,
            version,
            updated_at
        FROM agent_manager_agent_activities
        WHERE run_id = r.id
        ORDER BY started_at DESC, id DESC
        LIMIT 1
    ) activity ON TRUE
    WHERE (@scope_type::text IS NULL OR s.scope_type = @scope_type::text)
      AND (@scope_ref::text IS NULL OR s.scope_ref = @scope_ref::text)
      AND (@session_id::uuid IS NULL OR r.session_id = @session_id::uuid)
      AND (@role_profile_id::uuid IS NULL OR r.role_profile_id = @role_profile_id::uuid)
      AND (@status::text IS NULL OR r.status = @status::text)
      AND (@provider_work_item_ref::text IS NULL OR r.provider_target->>'work_item_ref' = @provider_work_item_ref::text)
      AND (@provider_pull_request_ref::text IS NULL OR r.provider_target->>'pull_request_ref' = @provider_pull_request_ref::text)
      AND (@created_after::timestamptz IS NULL OR r.created_at >= @created_after::timestamptz)
      AND (@created_before::timestamptz IS NULL OR r.created_at < @created_before::timestamptz)
)
SELECT
    id,
    session_id,
    flow_version_id,
    stage_id,
    role_profile_id,
    role_profile_version,
    role_profile_digest,
    prompt_template_version_id,
    prompt_template_digest,
    runtime_context,
    provider_target,
    guidance_refs,
    status,
    result_summary,
    failure_code,
    version,
    started_at,
    finished_at,
    created_at,
    updated_at,
    human_gate_request_ref,
    human_gate_reason_code,
    follow_up_ref,
    latest_activity_id,
    latest_activity_kind,
    latest_activity_status,
    latest_activity_tool_name,
    latest_activity_tool_category,
    latest_activity_safe_summary,
    latest_activity_payload_digest,
    latest_activity_bounded_error,
    latest_activity_started_at,
    latest_activity_finished_at,
    latest_activity_version,
    latest_activity_updated_at,
    sort_bucket,
    sort_time
FROM run_items
WHERE (
    @cursor_sort_bucket::int IS NULL
    OR sort_bucket > @cursor_sort_bucket::int
    OR (sort_bucket = @cursor_sort_bucket::int AND sort_time < @cursor_sort_time::timestamptz)
    OR (sort_bucket = @cursor_sort_bucket::int AND sort_time = @cursor_sort_time::timestamptz AND id < @cursor_id::uuid)
)
ORDER BY sort_bucket ASC, sort_time DESC, id DESC
LIMIT @limit::int
