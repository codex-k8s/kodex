-- name: context__issued_readback :one
SELECT internal_rpc_authority.read_issued_context_binding(
 @jti, @canonical_digest_sha256, @caller_workload_id, @target_workload_id,
 @source_revision, @source_digest_sha256, @key_set_revision, @policy_revision, @signer_generation,
 @issued_at, @expires_at, @parent_jti, @parent_digest_sha256);
