-- name: session_summary__list :many
WITH session_items AS (
    SELECT
        s.id,
        s.scope_type,
        s.scope_ref,
        s.provider_work_item_ref,
        s.flow_version_id,
        s.current_stage_id,
        s.latest_state_snapshot_id,
        s.status,
        s.created_by_actor_ref,
        s.version,
        s.created_at,
        s.updated_at,
        latest_run.id AS latest_run_id,
        latest_run.status AS latest_run_status,
        COALESCE(latest_run.runtime_context, '{}'::jsonb) AS latest_run_runtime_context,
        latest_run.result_summary AS latest_run_safe_summary,
        COALESCE(active_runs.active_run_count, 0)::int AS active_run_count,
        gate.id AS human_gate_request_ref,
        gate.reason_code AS human_gate_reason_code,
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
            WHEN COALESCE(active_runs.active_run_count, 0) > 0 THEN 1
            WHEN s.status IN ('open', 'waiting') THEN 2
            ELSE 3
        END AS sort_bucket,
        GREATEST(
            s.updated_at,
            COALESCE(latest_run.updated_at, s.updated_at),
            COALESCE(activity.started_at, s.updated_at)
        ) AS sort_time
    FROM agent_manager_sessions s
    LEFT JOIN LATERAL (
        SELECT id, status, runtime_context, result_summary, updated_at
        FROM agent_manager_runs
        WHERE session_id = s.id
        ORDER BY updated_at DESC, id DESC
        LIMIT 1
    ) latest_run ON TRUE
    LEFT JOIN LATERAL (
        SELECT COUNT(*) AS active_run_count
        FROM agent_manager_runs
        WHERE session_id = s.id
          AND status IN ('requested', 'starting', 'running', 'waiting')
    ) active_runs ON TRUE
    LEFT JOIN LATERAL (
        SELECT id, reason_code
        FROM agent_manager_human_gate_requests
        WHERE session_id = s.id
          AND status = 'waiting'
        ORDER BY updated_at DESC, id DESC
        LIMIT 1
    ) gate ON TRUE
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
        WHERE session_id = s.id
        ORDER BY started_at DESC, id DESC
        LIMIT 1
    ) activity ON TRUE
    WHERE (@scope_type::text IS NULL OR s.scope_type = @scope_type::text)
      AND (@scope_ref::text IS NULL OR s.scope_ref = @scope_ref::text)
      AND (@status::text IS NULL OR s.status = @status::text)
      AND (@provider_work_item_ref::text IS NULL OR s.provider_work_item_ref = @provider_work_item_ref::text)
      AND (@created_by_actor_ref::text IS NULL OR s.created_by_actor_ref = @created_by_actor_ref::text)
      AND (@created_after::timestamptz IS NULL OR s.created_at >= @created_after::timestamptz)
      AND (@created_before::timestamptz IS NULL OR s.created_at < @created_before::timestamptz)
)
SELECT
    id,
    scope_type,
    scope_ref,
    provider_work_item_ref,
    flow_version_id,
    current_stage_id,
    latest_state_snapshot_id,
    status,
    created_by_actor_ref,
    version,
    created_at,
    updated_at,
    latest_run_id,
    latest_run_status,
    latest_run_runtime_context,
    latest_run_safe_summary,
    active_run_count,
    human_gate_request_ref,
    human_gate_reason_code,
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
FROM session_items
WHERE (
    @cursor_sort_bucket::int IS NULL
    OR sort_bucket > @cursor_sort_bucket::int
    OR (sort_bucket = @cursor_sort_bucket::int AND sort_time < @cursor_sort_time::timestamptz)
    OR (sort_bucket = @cursor_sort_bucket::int AND sort_time = @cursor_sort_time::timestamptz AND id < @cursor_id::uuid)
)
ORDER BY sort_bucket ASC, sort_time DESC, id DESC
LIMIT @limit::int
