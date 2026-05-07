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
    remaining = EXCLUDED.remaining,
    limit_value = EXCLUDED.limit_value,
    reset_at = EXCLUDED.reset_at
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
