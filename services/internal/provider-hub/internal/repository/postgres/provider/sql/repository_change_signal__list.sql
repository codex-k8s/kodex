-- name: repository_change_signal__list :many
SELECT
    id,
    signal_key,
    kind,
    provider_slug,
    project_id,
    repository_id,
    repository_full_name,
    provider_repository_id,
    ref,
    base_branch,
    commit_sha,
    before_sha,
    source_ref,
    pull_request_number,
    pull_request_provider_id,
    pull_request_url,
    path_summary_status,
    changed_path_count,
    path_digest,
    path_categories_json,
    services_policy_changed,
    deploy_relevant_changed,
    change_fingerprint,
    observed_at,
    status,
    version,
    created_at,
    updated_at
FROM provider_hub_repository_change_signals
WHERE (@project_id::uuid IS NULL OR project_id = @project_id)
  AND (@repository_id::uuid IS NULL OR repository_id = @repository_id)
  AND (@provider_slug::text = '' OR provider_slug = @provider_slug)
  AND (@repository_full_name::text = '' OR repository_full_name = @repository_full_name)
  AND (@provider_repository_id::text = '' OR provider_repository_id = @provider_repository_id)
  AND (cardinality(@kinds::text[]) = 0 OR kind = ANY(@kinds::text[]))
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (@base_branch::text = '' OR base_branch = @base_branch)
  AND (@commit_sha::text = '' OR commit_sha = @commit_sha)
  AND (@services_policy_changed::boolean IS NULL OR services_policy_changed = @services_policy_changed)
  AND (@deploy_relevant_changed::boolean IS NULL OR deploy_relevant_changed = @deploy_relevant_changed)
  AND (@observed_since::timestamptz IS NULL OR observed_at >= @observed_since)
ORDER BY observed_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
