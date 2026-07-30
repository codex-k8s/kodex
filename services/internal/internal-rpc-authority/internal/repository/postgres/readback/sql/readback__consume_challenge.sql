-- name: readback__consume_challenge :one
WITH consumed AS (
    UPDATE internal_rpc_authority.authority_readback_attestation_challenges
    SET consumed_at = @accepted_at
    WHERE challenge_id = @challenge_id
      AND peer_spiffe_id = @peer_spiffe_id
      AND consumed_at IS NULL
      AND expires_at >= @accepted_at
      AND internal_rpc_authority.runtime_restore_fence_allows_work()
    RETURNING challenge_id
)
INSERT INTO internal_rpc_authority.authority_readback_attestation_receipts (
    receipt_id,
    challenge_id,
    semantic_request_digest_sha256,
    evidence_digest_sha256,
    verifier_generation,
    accepted_at,
    expires_at,
    evidence_jti,
    idempotency_key,
    peer_spiffe_id
)
SELECT
    @receipt_id,
    consumed.challenge_id,
    @semantic_request_digest_sha256,
    @evidence_digest_sha256,
    @verifier_generation,
    @accepted_at,
    @expires_at,
    @evidence_jti,
    @idempotency_key,
    @peer_spiffe_id
FROM consumed
RETURNING receipt_id;
