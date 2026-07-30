-- name: verifier__readiness :one
SELECT EXISTS (
    SELECT 1
    FROM internal_rpc_authority.authority_snapshot_watermarks
    WHERE target_workload_id = @target_workload_id
      AND source_revision = @source_revision
      AND source_digest_sha256 = @source_digest_sha256
      AND key_set_revision = @key_set_revision
      AND policy_revision = @policy_revision
      AND signer_generation = @signer_generation
)
AND internal_rpc_authority.runtime_restore_fence_allows_work();
