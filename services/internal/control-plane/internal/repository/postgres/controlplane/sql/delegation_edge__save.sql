-- name: DelegationEdgeSave
INSERT INTO control_plane.delegation_edges (
    id, organization_id, project_id, parent_process_run_id,
    source_session_id, source_turn_id, source_attempt, source_input_sha256,
    target_session_id, target_role_id, target_turn_id, target_attempt,
    target_input_sha256, root_initiator_actor_id, grant_generation, created_at
) VALUES (
    @id::uuid, @organization_id::uuid, @project_id::uuid,
    @parent_process_run_id::uuid, @source_session_id::uuid,
    @source_turn_id::uuid, @source_attempt, @source_input_sha256,
    @target_session_id::uuid, @target_role_id::uuid, @target_turn_id::uuid,
    @target_attempt, @target_input_sha256, @root_initiator_actor_id::uuid,
    @grant_generation, @created_at
)
