-- name: account_runtime_state__list :many
SELECT
    id,
    external_account_id,
    provider_slug,
    status,
    last_checked_at,
    last_success_at,
    last_error_code,
    last_error_message,
    version,
    created_at,
    updated_at
FROM provider_hub_account_runtime_states
WHERE (@provider_slug::text = '' OR provider_slug = @provider_slug)
  AND (cardinality(@external_account_ids::uuid[]) = 0 OR external_account_id = ANY(@external_account_ids::uuid[]))
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY provider_slug, external_account_id, id
LIMIT @limit::integer OFFSET @offset::integer;
