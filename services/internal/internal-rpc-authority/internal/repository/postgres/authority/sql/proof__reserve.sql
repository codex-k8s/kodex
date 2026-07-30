-- name: proof__reserve :one
WITH watermark AS (
    INSERT INTO internal_rpc_authority.authority_proof_watermarks (
        caller_workload_id,
        operation_id,
        authority_proof_issuer,
        proof_revision,
        canonical_payload_digest_sha256,
        updated_at
    )
    VALUES (
        @caller_workload_id,
        @operation_id,
        @authority_proof_issuer,
        @proof_revision,
        @canonical_digest_sha256,
        clock_timestamp()
    )
    ON CONFLICT (
        caller_workload_id,
        operation_id,
        authority_proof_issuer
    ) DO UPDATE
    SET proof_revision = EXCLUDED.proof_revision,
        canonical_payload_digest_sha256 = EXCLUDED.canonical_payload_digest_sha256,
        updated_at = EXCLUDED.updated_at
    WHERE internal_rpc_authority.authority_proof_watermarks.proof_revision < EXCLUDED.proof_revision
       OR (
           internal_rpc_authority.authority_proof_watermarks.proof_revision = EXCLUDED.proof_revision
           AND internal_rpc_authority.authority_proof_watermarks.canonical_payload_digest_sha256 =
               EXCLUDED.canonical_payload_digest_sha256
       )
    RETURNING true
),
reservation AS (
    INSERT INTO internal_rpc_authority.authority_proof_reservations (
        caller_workload_id,
        jti,
        canonical_digest_sha256,
        expires_at
    )
    SELECT
        @caller_workload_id,
        @jti,
        @canonical_digest_sha256,
        @expires_at
    FROM watermark
    ON CONFLICT (caller_workload_id, jti) DO NOTHING
    RETURNING true
)
SELECT EXISTS (SELECT 1 FROM reservation);
