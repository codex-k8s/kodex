-- name: proof__reserve :one
WITH fence AS (
    SELECT internal_rpc_authority.runtime_restore_fence_allows_work() AS open
),
watermark AS (
    INSERT INTO internal_rpc_authority.authority_proof_watermarks (
        caller_workload_id,
        operation_id,
        authority_proof_issuer,
        proof_revision,
        canonical_payload_digest_sha256,
        updated_at
    )
    SELECT
        @caller_workload_id,
        @operation_id,
        @authority_proof_issuer,
        @proof_revision,
        @canonical_digest_sha256,
        clock_timestamp()
    FROM fence
    WHERE fence.open
    ON CONFLICT (
        caller_workload_id,
        operation_id,
        authority_proof_issuer
    ) DO UPDATE
    -- Proof выдаются общей sequence, но конкурентные consumers предъявляют их
    -- не по порядку. Старый валидный proof не откатывает watermark; совпавшая
    -- revision с другим digest по-прежнему блокирует reservation.
    SET proof_revision = CASE
            WHEN internal_rpc_authority.authority_proof_watermarks.proof_revision < EXCLUDED.proof_revision
                THEN EXCLUDED.proof_revision
            ELSE internal_rpc_authority.authority_proof_watermarks.proof_revision
        END,
        canonical_payload_digest_sha256 = CASE
            WHEN internal_rpc_authority.authority_proof_watermarks.proof_revision < EXCLUDED.proof_revision
                THEN EXCLUDED.canonical_payload_digest_sha256
            ELSE internal_rpc_authority.authority_proof_watermarks.canonical_payload_digest_sha256
        END,
        updated_at = CASE
            WHEN internal_rpc_authority.authority_proof_watermarks.proof_revision < EXCLUDED.proof_revision
                THEN EXCLUDED.updated_at
            ELSE internal_rpc_authority.authority_proof_watermarks.updated_at
        END
    WHERE internal_rpc_authority.authority_proof_watermarks.proof_revision <> EXCLUDED.proof_revision
       OR internal_rpc_authority.authority_proof_watermarks.canonical_payload_digest_sha256 =
          EXCLUDED.canonical_payload_digest_sha256
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
