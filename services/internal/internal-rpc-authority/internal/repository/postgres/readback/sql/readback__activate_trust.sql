-- name: readback__activate_trust :one
SELECT internal_rpc_authority.activate_readback_trust(
    @root_id,
    @root_fingerprint_sha256,
    @manifest_bundle_revision,
    @manifest_bundle_digest_sha256,
    @trust_source_revision,
    @trust_set_digest_sha256,
    @trust_key_set_revision,
    @signer_generation,
    @predecessor_state_digest_sha256,
    @served_state_digest_sha256,
    @served_at
);
