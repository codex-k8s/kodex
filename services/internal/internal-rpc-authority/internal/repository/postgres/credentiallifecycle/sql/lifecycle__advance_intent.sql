-- name: lifecycle__advance_intent :one
UPDATE internal_rpc_authority.database_credential_rotation_intents
SET phase = @next_phase,
    pre_rotation_digests = CAST(@pre_rotation_digests AS jsonb),
    staged_digests = CAST(@staged_digests AS jsonb),
    updated_at = clock_timestamp()
WHERE request_id = @request_id
  AND canonical_digest_sha256 = @canonical_digest_sha256
  AND phase = @expected_phase
  AND EXISTS (
      SELECT 1
      FROM internal_rpc_authority.database_credential_reconciler_leases
      WHERE lease_name = 'database-credential-reconciler'
        AND holder_id = @holder_id
        AND fencing_token = @fencing_token
        AND lease_until > clock_timestamp()
  )
RETURNING
    request_id,
    canonical_digest_sha256,
    phase,
    pre_rotation_digests,
    staged_digests,
    created_at,
    updated_at;
