-- name: readback__resolve_intent :one
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
  AND workload_spiffe_id = @peer_spiffe_id
  AND status = 'PINNED'
  AND expires_at > pg_catalog.clock_timestamp()
  AND internal_rpc_authority.runtime_restore_fence_allows_work();
