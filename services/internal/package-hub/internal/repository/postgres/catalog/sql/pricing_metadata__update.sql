-- name: pricing_metadata__update :exec
UPDATE package_hub_pricing_metadata
SET pricing_kind = @pricing_kind,
    currency = @currency,
    price_payload = @price_payload::jsonb,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND package_id = @package_id
  AND version = @previous_version;
