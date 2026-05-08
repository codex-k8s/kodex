-- name: workspace_materialization__update :exec
UPDATE runtime_manager_workspace_materializations
SET
    status = @status,
    policy_digest = @policy_digest,
    sources_json = @sources_json::jsonb,
    fingerprint = @fingerprint,
    started_at = @started_at::timestamptz,
    finished_at = @finished_at::timestamptz,
    last_error_code = @last_error_code,
    last_error_message = @last_error_message,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
