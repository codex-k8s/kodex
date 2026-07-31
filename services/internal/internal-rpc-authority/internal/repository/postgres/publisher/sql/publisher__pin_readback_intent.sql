-- name: publisher__pin_readback_intent :one
INSERT INTO internal_rpc_authority.authority_readback_intents (
    intent_id,
    kind,
    intent_revision,
    intent_digest_sha256,
    workload_id,
    workload_spiffe_id,
    role,
    workload_generation,
    credential_generation,
    material_generation,
    possession_key_generation,
    possession_key_kid,
    possession_key_generation_exact,
    possession_public_jwk,
    possession_key_thumbprint_sha256,
    source_revision,
    served_state_digest_sha256,
    status,
    expires_at
)
VALUES (
    @intent_id,
    @kind,
    @intent_revision,
    @intent_digest_sha256,
    @workload_id,
    @workload_spiffe_id,
    @role,
    @workload_generation,
    @credential_generation,
    @material_generation,
    @possession_key_generation,
    @possession_key_kid,
    @possession_key_generation,
    @possession_public_jwk,
    @possession_key_thumbprint_sha256,
    @source_revision,
    @served_state_digest_sha256,
    'PINNED',
    @expires_at
)
ON CONFLICT (intent_id) DO UPDATE
SET intent_id = internal_rpc_authority.authority_readback_intents.intent_id
WHERE internal_rpc_authority.authority_readback_intents.intent_digest_sha256 =
        EXCLUDED.intent_digest_sha256
  AND internal_rpc_authority.authority_readback_intents.workload_spiffe_id =
        EXCLUDED.workload_spiffe_id
  AND internal_rpc_authority.authority_readback_intents.status = 'PINNED'
RETURNING
    intent_id,
    kind,
    intent_revision,
    intent_digest_sha256,
    workload_id,
    workload_spiffe_id,
    role,
    workload_generation,
    credential_generation,
    material_generation,
    possession_key_kid,
    possession_key_generation_exact,
    possession_public_jwk,
    possession_key_thumbprint_sha256,
    source_revision,
    served_state_digest_sha256,
    status,
    expires_at;
