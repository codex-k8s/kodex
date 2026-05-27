-- name: repository_adoption_scan__list :many
SELECT
    scan.id,
    scan.snapshot_key,
    scan.provider_operation_id,
    scan.external_account_id,
    scan.provider_slug,
    scan.repository_full_name,
    scan.provider_repository_id,
    scan.repository_url,
    scan.default_branch,
    scan.requested_ref,
    scan.scanned_ref,
    scan.head_sha,
    scan.status,
    scan.markers_json,
    scan.file_count,
    scan.visible_file_count,
    scan.tree_truncated,
    scan.warnings_json,
    scan.snapshot_digest,
    scan.observed_at,
    scan.version,
    scan.created_at,
    scan.updated_at
FROM provider_hub_repository_adoption_scan_snapshots AS scan
JOIN provider_hub_operations AS operation
    ON operation.id = scan.provider_operation_id
WHERE (@project_id::text = '' OR operation.operation_policy_context_json->>'project_id' = @project_id)
  AND (@repository_id::text = '' OR operation.operation_policy_context_json->>'repository_id' = @repository_id)
  AND (@external_account_id::uuid IS NULL OR scan.external_account_id = @external_account_id)
  AND (@provider_slug::text = '' OR scan.provider_slug = @provider_slug)
  AND (@repository_full_name::text = '' OR scan.repository_full_name = @repository_full_name)
  AND (@provider_repository_id::text = '' OR scan.provider_repository_id = @provider_repository_id)
  AND (cardinality(@statuses::text[]) = 0 OR scan.status = ANY(@statuses::text[]))
  AND (@observed_since::timestamptz IS NULL OR scan.observed_at >= @observed_since)
ORDER BY scan.observed_at DESC, scan.id
LIMIT @limit::integer OFFSET @offset::integer;
