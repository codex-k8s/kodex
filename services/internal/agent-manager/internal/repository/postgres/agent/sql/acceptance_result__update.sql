-- name: acceptance_result__update :exec
UPDATE agent_manager_acceptance_results
SET
    session_id = @session_id,
    run_id = @run_id::uuid,
    stage_id = @stage_id::uuid,
    check_kind = @check_kind,
    status = @status,
    target_ref = @target_ref,
    details_json = @details_json::jsonb,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
