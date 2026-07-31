-- name: readback__load_challenge :one
SELECT
    challenge.challenge_id,
    challenge.challenge_jti,
    challenge.nonce,
    challenge.challenge_digest_sha256,
    challenge.readback_credential_jti,
    challenge.readback_credential_digest_sha256,
    challenge.idempotency_key,
    challenge.semantic_request_digest_sha256,
    challenge.issued_at,
    challenge.expires_at,
    challenge.consumed_at,
    intent.intent_id,
    intent.kind,
    intent.intent_revision,
    intent.intent_digest_sha256,
    intent.workload_id,
    intent.workload_spiffe_id,
    intent.role,
    intent.workload_generation,
    intent.credential_generation,
    intent.material_generation,
    intent.possession_key_kid,
    intent.possession_key_generation_exact,
    intent.possession_public_jwk,
    intent.possession_key_thumbprint_sha256,
    intent.source_revision,
    intent.served_state_digest_sha256,
    intent.status,
    intent.expires_at
FROM internal_rpc_authority.authority_readback_attestation_challenges AS challenge
JOIN internal_rpc_authority.authority_readback_intents AS intent
  ON intent.intent_id = challenge.intent_id
WHERE challenge.challenge_id = @challenge_id
  AND challenge.peer_spiffe_id = @peer_spiffe_id
  AND intent.workload_spiffe_id = @peer_spiffe_id
  AND internal_rpc_authority.runtime_restore_fence_allows_work();
