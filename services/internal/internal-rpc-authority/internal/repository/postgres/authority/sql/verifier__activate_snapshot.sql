-- name: verifier__activate_snapshot :one
WITH accepted_snapshot AS (
    INSERT INTO internal_rpc_authority.verifier_served_snapshots (
        target_workload_id,
        source_revision,
        source_digest_sha256,
        key_set_revision,
        policy_revision,
        signer_generation,
        served_at
    )
    SELECT
        @target_workload_id,
        @source_revision,
        @source_digest_sha256,
        @key_set_revision,
        @policy_revision,
        @signer_generation,
        clock_timestamp()
    WHERE (
        @source_revision = 1
        AND @predecessor_revision = 0
        AND @predecessor_digest_sha256 = '0000000000000000000000000000000000000000000000000000000000000000'
    )
    OR EXISTS (
        SELECT 1
        FROM internal_rpc_authority.verifier_served_snapshots AS current
        WHERE current.target_workload_id = @target_workload_id
          AND current.key_set_revision <= @key_set_revision
          AND current.policy_revision <= @policy_revision
          AND current.signer_generation <= @signer_generation
          AND (
              (
                  current.source_revision = @source_revision
                  AND current.source_digest_sha256 = @source_digest_sha256
              )
              OR (
                  current.source_revision + 1 = @source_revision
                  AND current.source_revision = @predecessor_revision
                  AND current.source_digest_sha256 = @predecessor_digest_sha256
              )
          )
    )
    ON CONFLICT (target_workload_id) DO UPDATE
    SET source_revision = EXCLUDED.source_revision,
        source_digest_sha256 = EXCLUDED.source_digest_sha256,
        key_set_revision = EXCLUDED.key_set_revision,
        policy_revision = EXCLUDED.policy_revision,
        signer_generation = EXCLUDED.signer_generation,
        served_at = EXCLUDED.served_at
    WHERE internal_rpc_authority.verifier_served_snapshots.source_revision <= EXCLUDED.source_revision
      AND internal_rpc_authority.verifier_served_snapshots.key_set_revision <= EXCLUDED.key_set_revision
      AND internal_rpc_authority.verifier_served_snapshots.policy_revision <= EXCLUDED.policy_revision
      AND internal_rpc_authority.verifier_served_snapshots.signer_generation <= EXCLUDED.signer_generation
      AND (
          (
              internal_rpc_authority.verifier_served_snapshots.source_revision + 1 = EXCLUDED.source_revision
              AND @predecessor_revision = internal_rpc_authority.verifier_served_snapshots.source_revision
              AND @predecessor_digest_sha256 = internal_rpc_authority.verifier_served_snapshots.source_digest_sha256
          )
          OR internal_rpc_authority.verifier_served_snapshots.source_digest_sha256 = EXCLUDED.source_digest_sha256
      )
    RETURNING true
)
SELECT EXISTS (SELECT 1 FROM accepted_snapshot);
