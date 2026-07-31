-- name: restore_fence__readiness :one
SELECT EXISTS (
    SELECT 1
    FROM internal_rpc_authority.authority_restore_fences
    WHERE database_cluster_id = @database_cluster_id
      AND restore_epoch = @restore_epoch
      AND phase = @phase
      AND evidence_digest_sha256 = @evidence_digest_sha256
      AND safe_window_not_before IS NOT DISTINCT FROM @safe_window_not_before
);
