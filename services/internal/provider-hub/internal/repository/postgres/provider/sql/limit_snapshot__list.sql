-- name: limit_snapshot__list :many
SELECT
    id,
    external_account_id,
    provider_slug,
    limit_class,
    remaining,
    limit_value,
    reset_at,
    captured_at,
    source
FROM provider_hub_limit_snapshots
WHERE (@external_account_id::uuid IS NULL OR external_account_id = @external_account_id)
  AND (@provider_slug::text = '' OR provider_slug = @provider_slug)
  AND (cardinality(@limit_classes::text[]) = 0 OR limit_class = ANY(@limit_classes::text[]))
  AND (@captured_since::timestamptz IS NULL OR captured_at >= @captured_since)
ORDER BY captured_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
