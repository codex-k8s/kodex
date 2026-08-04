SELECT id FROM interaction_gateway_deliveries
WHERE provider_post_id = $1 AND state IN ('PROVIDER_ACCEPTED', 'DELIVERED')
ORDER BY updated_at DESC LIMIT 1;
