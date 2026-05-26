-- name: acceptance_result__create :exec
INSERT INTO agent_manager_acceptance_results (
    id, session_id, run_id, stage_id, check_kind, status, target_ref,
    details_json, version, created_at, updated_at
) VALUES (
    @id, @session_id, @run_id::uuid, @stage_id::uuid, @check_kind, @status, @target_ref,
    @details_json::jsonb, @version, @created_at, @updated_at
);
