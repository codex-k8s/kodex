-- name: acceptance_result__get :one
SELECT
    id,
    session_id,
    run_id,
    stage_id,
    check_kind,
    status,
    target_ref,
    details_json,
    version,
    created_at,
    updated_at
FROM agent_manager_acceptance_results
WHERE id = @id;
