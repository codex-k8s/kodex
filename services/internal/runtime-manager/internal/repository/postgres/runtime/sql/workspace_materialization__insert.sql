-- name: workspace_materialization__insert :exec
INSERT INTO runtime_manager_workspace_materializations (
    id,
    slot_id,
    status,
    policy_digest,
    sources_json,
    fingerprint,
    started_at,
    finished_at,
    last_error_code,
    last_error_message,
    version,
    created_at,
    updated_at
) VALUES (
    @id,
    @slot_id,
    @status,
    @policy_digest,
    @sources_json::jsonb,
    @fingerprint,
    @started_at::timestamptz,
    @finished_at::timestamptz,
    @last_error_code,
    @last_error_message,
    @version,
    @created_at,
    @updated_at
);
