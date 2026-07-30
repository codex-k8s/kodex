-- name: publisher__promote_snapshot :one
SELECT internal_rpc_authority.publisher_promote_snapshot(
    @publication_intent_id,
    @source_revision,
    @source_digest_sha256,
    @expected_readback_count
);
