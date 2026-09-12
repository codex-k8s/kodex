-- name: publisher__mark_rotation_delivered :one
SELECT internal_rpc_authority.publisher_mark_rotation_delivered(
    @intent_id,
    @source_revision,
    @source_digest_sha256
);
