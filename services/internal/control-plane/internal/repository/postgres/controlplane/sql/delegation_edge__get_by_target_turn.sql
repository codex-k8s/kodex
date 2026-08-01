-- name: DelegationEdgeGetByTargetTurn
SELECT
    id::text, organization_id::text, project_id::text,
    parent_process_run_id::text, source_session_id::text, source_turn_id::text,
    source_attempt, source_input_sha256, target_session_id::text,
    target_role_id::text, target_turn_id::text, target_attempt,
    target_input_sha256, root_initiator_actor_id::text, grant_generation,
    created_at
FROM control_plane.delegation_edges
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND target_turn_id = @target_turn_id::uuid
FOR UPDATE
