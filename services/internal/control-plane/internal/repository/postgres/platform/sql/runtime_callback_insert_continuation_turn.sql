-- name: runtime_callback_insert_continuation_turn :one
INSERT INTO control_plane.session_turns(
    ref, organization_id, session_id, run_id, turn_number, actor_kind,
    actor_ref, content, state
) VALUES (
    @turn_ref, @organization_id::uuid, @session_id::uuid, @parent_run_id::uuid,
    @turn_number, 'AGENT', @agent_ref, @content, 'QUEUED'
)
RETURNING id::text
