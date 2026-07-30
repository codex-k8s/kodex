-- name: context__reserve :one
INSERT INTO internal_rpc_authority.authority_replay_reservations (
    target_workload_id,
    jti,
    canonical_digest_sha256,
    expires_at
)
SELECT
    @target_workload_id,
    @jti,
    @canonical_digest_sha256,
    @expires_at
WHERE internal_rpc_authority.runtime_restore_fence_allows_work()
ON CONFLICT (target_workload_id, jti) DO NOTHING
RETURNING true;
