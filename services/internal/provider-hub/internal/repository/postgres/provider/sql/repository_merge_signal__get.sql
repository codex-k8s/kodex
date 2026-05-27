-- name: repository_merge_signal__get :one
SELECT
    id,
    signal_key,
    kind,
    provider_slug,
    project_id,
    repository_id,
    repository_full_name,
    provider_repository_id,
    work_item_projection_id,
    provider_work_item_id,
    pull_request_number,
    pull_request_provider_id,
    pull_request_url,
    base_branch,
    head_branch,
    merge_commit_sha,
    source_ref,
    related_provider_operation_ref,
    watermark_digest,
    observed_at,
    merged_at,
    status,
    version,
    created_at,
    updated_at
FROM provider_hub_repository_merge_signals
WHERE (@id::uuid IS NULL OR id = @id)
  AND (@signal_key::text = '' OR signal_key = @signal_key);
