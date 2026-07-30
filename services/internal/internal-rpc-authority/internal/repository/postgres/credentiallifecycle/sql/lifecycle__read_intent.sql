-- name: lifecycle__read_intent :one
SELECT
    request_id,
    canonical_digest_sha256,
    phase,
    pre_rotation_digests,
    staged_digests,
    created_at,
    updated_at
FROM internal_rpc_authority.database_credential_rotation_intents
WHERE request_id = @request_id
  AND canonical_digest_sha256 = @canonical_digest_sha256;
