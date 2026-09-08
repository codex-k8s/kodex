-- name: freshness__activate :one
SELECT internal_rpc_authority.activate_authority_freshness($1, $2::uuid);
