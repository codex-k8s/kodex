-- name: blocking_signal__create :exec
INSERT INTO governance_manager_blocking_signals (
    id, target_type, target_ref, source_type, source_ref, severity,
    summary, status, version, created_at, updated_at, resolved_at
) VALUES (
    @id, @target_type, @target_ref, @source_type, @source_ref, @severity,
    @summary, @status, @version, @created_at, @updated_at, @resolved_at
);
