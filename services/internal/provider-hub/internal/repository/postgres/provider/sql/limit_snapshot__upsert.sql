-- name: limit_snapshot__upsert :one
INSERT INTO provider_hub_limit_snapshots (
    id,
    external_account_id,
    provider_slug,
    limit_class,
    remaining,
    limit_value,
    reset_at,
    captured_at,
    source
) VALUES (
    @id,
    @external_account_id,
    @provider_slug,
    @limit_class,
    @remaining,
    @limit_value,
    @reset_at,
    @captured_at,
    @source
)
ON CONFLICT (external_account_id, provider_slug, limit_class, captured_at, source) DO UPDATE SET
    remaining = provider_hub_limit_snapshots.remaining
WHERE provider_hub_limit_snapshots.remaining IS NOT DISTINCT FROM EXCLUDED.remaining
    AND provider_hub_limit_snapshots.limit_value IS NOT DISTINCT FROM EXCLUDED.limit_value
    AND provider_hub_limit_snapshots.reset_at IS NOT DISTINCT FROM EXCLUDED.reset_at
RETURNING
    id,
    external_account_id,
    provider_slug,
    limit_class,
    remaining,
    limit_value,
    reset_at,
    captured_at,
    source;
