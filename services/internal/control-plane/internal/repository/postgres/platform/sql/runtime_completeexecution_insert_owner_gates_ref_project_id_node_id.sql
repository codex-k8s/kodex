-- name: runtime_completeexecution_insert_owner_gates_ref_project_id_node_id :one
WITH opened AS MATERIALIZED (SELECT clock_timestamp() AS at)
INSERT INTO control_plane.owner_gates (
    ref,
    organization_id,
    project_id,
    root_run_id,
    node_id,
    title,
    prompt,
    context_summary,
    allowed_decisions,
    state,
    expires_at,
    created_at
)
SELECT
    @gate_ref,
    @organization_id::uuid,
    @project_id::uuid,
    @root_run_id::uuid,
    @node_id::uuid,
    'i18n:OWNER_GATE_REVIEW_TITLE',
    'i18n:OWNER_GATE_REVIEW_PROMPT',
    @context_summary,
    @allowed_decisions::text[],
    'OPEN',
    CASE WHEN @approval_timeout_seconds::integer IS NULL THEN NULL
         ELSE opened.at+make_interval(secs=>@approval_timeout_seconds::integer) END,
    opened.at
FROM opened
RETURNING id::text
