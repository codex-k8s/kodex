-- name: publisher__load_snapshot_predecessor :one
SELECT *
FROM internal_rpc_authority.publisher_load_snapshot_predecessor(
    @source_revision,
    @source_digest_sha256,
    @registry_digest_sha256
);
