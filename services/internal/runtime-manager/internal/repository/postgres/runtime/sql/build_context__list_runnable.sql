-- name: build_context__list_runnable :many
SELECT
    id,
    status,
    project_id,
    repository_id,
    provider,
    provider_owner,
    provider_name,
    source_ref,
    source_commit_sha,
    affected_service_keys_json,
    build_plan_fingerprint,
    context_fingerprint,
    source_snapshot_ref,
    source_snapshot_digest,
    build_context_ref,
    build_context_digest,
    manifest_bundle_digests_json,
    started_at,
    finished_at,
    last_error_code,
    last_error_message,
    next_action,
    version,
    created_at,
    updated_at
FROM runtime_manager_build_contexts
WHERE status IN ('pending', 'running')
ORDER BY created_at ASC, id ASC
LIMIT @limit::int
