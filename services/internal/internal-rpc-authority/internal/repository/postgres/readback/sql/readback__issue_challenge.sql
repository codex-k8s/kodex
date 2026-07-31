-- name: readback__issue_challenge :one
SELECT internal_rpc_authority.issue_authority_readback_attestation_challenge(
    @intent_id,
    @challenge_id,
    @challenge_jti,
    @challenge_nonce,
    @challenge_digest_sha256,
    @readback_credential_jti,
    @readback_credential_digest_sha256,
    @idempotency_key,
    @semantic_request_digest_sha256
);
