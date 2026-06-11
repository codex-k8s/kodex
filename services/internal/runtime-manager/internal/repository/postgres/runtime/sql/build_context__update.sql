-- name: build_context__update :exec
UPDATE runtime_manager_build_contexts
SET
    status = @status,
    source_snapshot_ref = @source_snapshot_ref,
    source_snapshot_digest = @source_snapshot_digest,
    build_context_ref = @build_context_ref,
    build_context_digest = @build_context_digest,
    started_at = @started_at,
    finished_at = @finished_at,
    last_error_code = @last_error_code,
    last_error_message = @last_error_message,
    next_action = @next_action,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version
