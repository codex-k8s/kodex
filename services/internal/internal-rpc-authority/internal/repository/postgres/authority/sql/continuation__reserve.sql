-- name: continuation__reserve :one
WITH parent AS MATERIALIZED (
    SELECT true AS accepted
    FROM internal_rpc_authority.authority_replay_reservations
    WHERE target_workload_id = @parent_target_workload_id
      AND jti = @parent_jti
      AND canonical_digest_sha256 = @parent_canonical_digest_sha256
      AND expires_at > clock_timestamp()
      AND internal_rpc_authority.runtime_restore_fence_allows_work()
), child AS (
    INSERT INTO internal_rpc_authority.authority_proof_reservations (
        caller_workload_id,
        jti,
        canonical_digest_sha256,
        expires_at
    )
    SELECT
        @caller_workload_id,
        @jti,
        @canonical_digest_sha256,
        @expires_at
    FROM parent
    ON CONFLICT (caller_workload_id, jti) DO NOTHING
    RETURNING true AS accepted
)
SELECT EXISTS (SELECT 1 FROM parent), EXISTS (SELECT 1 FROM child);
