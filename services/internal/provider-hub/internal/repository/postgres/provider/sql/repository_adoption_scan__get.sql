-- name: repository_adoption_scan__get :one
SELECT
    id,
    snapshot_key,
    provider_operation_id,
    external_account_id,
    provider_slug,
    repository_full_name,
    provider_repository_id,
    repository_url,
    default_branch,
    requested_ref,
    scanned_ref,
    head_sha,
    status,
    markers_json,
    file_count,
    visible_file_count,
    tree_truncated,
    warnings_json,
    snapshot_digest,
    observed_at,
    version,
    created_at,
    updated_at
FROM provider_hub_repository_adoption_scan_snapshots
WHERE (@id::uuid IS NULL OR id = @id)
  AND (@snapshot_key::text = '' OR snapshot_key = @snapshot_key)
  AND (@provider_operation_id::uuid IS NULL OR provider_operation_id = @provider_operation_id);
