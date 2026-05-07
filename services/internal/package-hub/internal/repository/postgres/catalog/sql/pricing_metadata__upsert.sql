-- name: pricing_metadata__upsert :exec
INSERT INTO package_hub_pricing_metadata (
    id,
    package_id,
    pricing_kind,
    currency,
    price_payload,
    version,
    updated_at
) VALUES (
    @id,
    @package_id,
    @pricing_kind,
    @currency,
    @price_payload::jsonb,
    @version,
    @updated_at
)
ON CONFLICT (package_id) DO UPDATE SET
    id = EXCLUDED.id,
    pricing_kind = EXCLUDED.pricing_kind,
    currency = EXCLUDED.currency,
    price_payload = EXCLUDED.price_payload,
    version = EXCLUDED.version,
    updated_at = EXCLUDED.updated_at;
