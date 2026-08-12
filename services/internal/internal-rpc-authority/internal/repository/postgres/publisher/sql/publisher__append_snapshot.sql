-- name: publisher__append_snapshot :one
SELECT internal_rpc_authority.publisher_append_snapshot_history(
    @source_revision,
    @source_digest_sha256,
    @key_set_revision,
    @policy_revision,
    @signer_generation,
    @predecessor_revision,
    @predecessor_digest_sha256,
    @snapshot_compact_jws,
    @publication_intent_id,
    @publication_input_digest_sha256,
    @expected_readback_count,
    @published_at
);
