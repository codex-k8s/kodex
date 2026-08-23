-- name: runtime_completeexecution_insert_owner_gates_ref_project_id_node_id :one
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
    state
)
VALUES (
    @gate_ref,
    @organization_id::uuid,
    @project_id::uuid,
    @root_run_id::uuid,
    @node_id::uuid,
    'i18n:OWNER_GATE_REVIEW_TITLE',
    'i18n:OWNER_GATE_REVIEW_PROMPT',
    @context_summary,
    ARRAY['APPROVE', 'REJECT', 'REQUEST_CHANGES', 'CANCEL'],
    'OPEN'
)
RETURNING id::text
