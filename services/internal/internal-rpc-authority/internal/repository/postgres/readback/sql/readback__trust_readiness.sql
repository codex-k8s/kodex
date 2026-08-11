-- name: readback__trust_readiness :one
SELECT EXISTS (
    SELECT 1
    FROM internal_rpc_authority.authority_readback_trust_watermarks
    WHERE attestor_id = 'internal-rpc-authority-readback-attestor'
      AND root_id = @root_id
      AND root_fingerprint_sha256 = @root_fingerprint_sha256
      AND manifest_bundle_revision = @manifest_bundle_revision
      AND manifest_bundle_digest_sha256 = @manifest_bundle_digest_sha256
      AND trust_source_revision = @trust_source_revision
      AND trust_set_digest_sha256 = @trust_set_digest_sha256
      AND trust_key_set_revision = @trust_key_set_revision
      AND signer_generation = @signer_generation
      AND served_state_digest_sha256 = @served_state_digest_sha256
)
AND pg_catalog.has_function_privilege(
    current_user,
    'internal_rpc_authority.activate_readback_trust(text,text,bigint,text,bigint,text,bigint,bigint,text,text,timestamptz)',
    'EXECUTE'
);
