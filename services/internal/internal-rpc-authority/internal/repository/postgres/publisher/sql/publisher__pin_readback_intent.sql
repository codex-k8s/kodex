-- name: publisher__pin_readback_intent :one
WITH inserted AS (
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
ON CONFLICT (intent_id) DO NOTHING
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
    expires_at
)
SELECT * FROM inserted
UNION ALL
SELECT
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
    expires_at
FROM internal_rpc_authority.authority_readback_intents
WHERE intent_id = @intent_id
  AND intent_digest_sha256 = @intent_digest_sha256
  AND workload_spiffe_id = @workload_spiffe_id
  AND status = 'PINNED'
  AND NOT EXISTS (SELECT 1 FROM inserted)
LIMIT 1;
