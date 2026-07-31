-- name: lifecycle__load_or_create_intent :one
WITH inserted AS (
    INSERT INTO internal_rpc_authority.database_credential_rotation_intents (
        request_id,
        canonical_digest_sha256,
        phase
    )
    SELECT
        @request_id,
        @canonical_digest_sha256,
        'CREATED'
    WHERE EXISTS (
        SELECT 1
        FROM internal_rpc_authority.database_credential_reconciler_leases
        WHERE lease_name = 'database-credential-reconciler'
          AND holder_id = @holder_id
          AND fencing_token = @fencing_token
          AND lease_until > clock_timestamp()
    )
    ON CONFLICT (request_id) DO NOTHING
)
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
  AND canonical_digest_sha256 = @canonical_digest_sha256
  AND EXISTS (
      SELECT 1
      FROM internal_rpc_authority.database_credential_reconciler_leases
      WHERE lease_name = 'database-credential-reconciler'
        AND holder_id = @holder_id
        AND fencing_token = @fencing_token
        AND lease_until > clock_timestamp()
  )
FOR UPDATE;
