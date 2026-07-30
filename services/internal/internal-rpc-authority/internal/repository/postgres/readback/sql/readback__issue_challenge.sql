-- name: readback__issue_challenge :one
INSERT INTO internal_rpc_authority.authority_readback_attestation_challenges (
    challenge_id,
    challenge_jti,
    intent_id,
    request_digest_sha256,
    nonce,
    issued_at,
    expires_at,
    peer_spiffe_id,
    readback_credential_jti,
    readback_credential_digest_sha256,
    idempotency_key,
    semantic_request_digest_sha256,
    challenge_digest_sha256
)
VALUES (
    @challenge_id,
    @challenge_jti,
    @intent_id,
    @semantic_request_digest_sha256,
    @challenge_nonce,
    @issued_at,
    @expires_at,
    @peer_spiffe_id,
    @readback_credential_jti,
    @readback_credential_digest_sha256,
    @idempotency_key,
    @semantic_request_digest_sha256,
    @challenge_digest_sha256
)
ON CONFLICT (peer_spiffe_id, idempotency_key) DO UPDATE
SET semantic_request_digest_sha256 =
        internal_rpc_authority.authority_readback_attestation_challenges.semantic_request_digest_sha256
WHERE internal_rpc_authority.authority_readback_attestation_challenges.semantic_request_digest_sha256 =
        EXCLUDED.semantic_request_digest_sha256
  AND internal_rpc_authority.authority_readback_attestation_challenges.intent_id =
        EXCLUDED.intent_id
  AND internal_rpc_authority.authority_readback_attestation_challenges.readback_credential_digest_sha256 =
        EXCLUDED.readback_credential_digest_sha256
RETURNING challenge_id;
