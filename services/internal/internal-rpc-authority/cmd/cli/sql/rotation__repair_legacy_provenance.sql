-- name: rotation__repair_legacy_provenance :one
SELECT internal_rpc_authority.repair_legacy_registry_provenance($1, $2, $3);
