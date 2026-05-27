-- name: repository_merge_signal__list :many
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
WHERE (@project_id::uuid IS NULL OR project_id = @project_id)
  AND (@repository_id::uuid IS NULL OR repository_id = @repository_id)
  AND (@provider_slug::text = '' OR provider_slug = @provider_slug)
  AND (@repository_full_name::text = '' OR repository_full_name = @repository_full_name)
  AND (@provider_repository_id::text = '' OR provider_repository_id = @provider_repository_id)
  AND (cardinality(@kinds::text[]) = 0 OR kind = ANY(@kinds::text[]))
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (@pull_request_number::bigint IS NULL OR pull_request_number = @pull_request_number)
  AND (@merged_since::timestamptz IS NULL OR merged_at >= @merged_since)
ORDER BY merged_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
