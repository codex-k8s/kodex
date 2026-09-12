-- name: publisher__prepare_rotation :one
SELECT internal_rpc_authority.publisher_prepare_rotation(
    @intent_id,
    @source_revision,
    @source_digest_sha256,
    @predecessor_revision,
    @predecessor_digest_sha256,
    @expected_readback_count
);
