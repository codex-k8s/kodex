-- name: readback__consume_challenge :one
SELECT internal_rpc_authority.consume_authority_readback_attestation_challenge(
    @challenge_id,
    @receipt_id,
    @evidence_jti,
    @evidence_digest_sha256,
    @verifier_generation,
    @idempotency_key,
    @semantic_request_digest_sha256
);
