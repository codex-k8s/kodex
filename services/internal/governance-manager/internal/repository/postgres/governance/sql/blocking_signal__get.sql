-- name: blocking_signal__get :one
SELECT
    id, target_type, target_ref, source_type, source_ref, severity,
    summary, status, version, created_at, updated_at, resolved_at
FROM governance_manager_blocking_signals
WHERE id = @id;
