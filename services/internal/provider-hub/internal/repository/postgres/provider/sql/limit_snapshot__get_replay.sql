-- name: limit_snapshot__get_replay :one
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
WHERE external_account_id = @external_account_id
    AND provider_slug = @provider_slug
    AND limit_class = @limit_class
    AND captured_at = @captured_at
    AND source = @source
    AND remaining IS NOT DISTINCT FROM @remaining
    AND limit_value IS NOT DISTINCT FROM @limit_value
    AND reset_at IS NOT DISTINCT FROM @reset_at;
