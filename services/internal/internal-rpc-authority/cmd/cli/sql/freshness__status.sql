-- name: freshness__status :one
SELECT version, maximum_age_seconds, activation_id::text, activated_at,
 clock_timestamp(), internal_rpc_authority.authority_freshness_consumer_status()
FROM internal_rpc_authority.authority_freshness_status();
