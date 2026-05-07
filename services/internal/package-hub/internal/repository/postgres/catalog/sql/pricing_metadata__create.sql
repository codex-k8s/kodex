-- name: pricing_metadata__create :exec
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
);
