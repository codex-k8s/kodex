-- name: runtime_callback_insert_completed_turn :one
INSERT INTO control_plane.session_turns(
    ref, organization_id, session_id, run_id, turn_number, actor_kind,
    actor_ref, content, state, completed_at
) VALUES (
    @turn_ref, @organization_id::uuid, @session_id::uuid, @parent_run_id::uuid,
    @turn_number, 'AGENT', @child_run_ref, @content, 'COMPLETED', clock_timestamp()
)
RETURNING id::text
