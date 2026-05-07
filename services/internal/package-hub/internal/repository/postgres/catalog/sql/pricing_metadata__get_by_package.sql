-- name: pricing_metadata__get_by_package :one
SELECT
    id,
    package_id,
    pricing_kind,
    currency,
    price_payload,
    version,
    updated_at
FROM package_hub_pricing_metadata
WHERE package_id = @package_id;
