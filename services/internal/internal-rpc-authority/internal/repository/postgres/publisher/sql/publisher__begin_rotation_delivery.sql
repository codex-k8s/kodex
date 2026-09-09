-- name: publisher__begin_rotation_delivery :one
SELECT internal_rpc_authority.publisher_begin_rotation_delivery(
    @intent_id,
    @source_revision,
    @source_digest_sha256
);
