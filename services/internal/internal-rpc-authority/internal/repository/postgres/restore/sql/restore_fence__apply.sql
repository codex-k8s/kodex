-- name: restore_fence__apply :one
SELECT internal_rpc_authority.apply_restore_fence(
    @database_cluster_id,
    @restore_epoch,
    @phase,
    @evidence_digest_sha256,
    @safe_window_not_before
);
