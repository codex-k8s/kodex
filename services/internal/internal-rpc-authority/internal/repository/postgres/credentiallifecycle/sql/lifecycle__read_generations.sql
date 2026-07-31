-- name: lifecycle__read_generations :many
SELECT
    capability,
    generation,
    lifecycle_status,
    principal
FROM internal_rpc_authority.authority_runtime_database_identities
WHERE registered_set_digest_sha256 = @registered_set_digest_sha256
  AND lifecycle_status IN ('CURRENT', 'NEXT', 'PREVIOUS', 'RETIRED')
ORDER BY capability, generation;
