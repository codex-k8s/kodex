-- name: verifier__accept_context :one
WITH accepted_snapshot AS (
    INSERT INTO internal_rpc_authority.authority_snapshot_watermarks (
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
    WHERE internal_rpc_authority.runtime_restore_fence_allows_work()
      AND (
          (
              @source_revision = 1
              AND @predecessor_revision = 0
              AND @predecessor_digest_sha256 =
                  '0000000000000000000000000000000000000000000000000000000000000000'
          )
          OR EXISTS (
        SELECT 1
        FROM internal_rpc_authority.authority_snapshot_watermarks AS current
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
                  current.source_revision < @source_revision
                  AND EXISTS (
                      SELECT 1
                      FROM unnest(
                          @history_revisions::bigint[],
                          @history_digests::text[]
                      ) AS signed_history(revision, digest_sha256)
                      WHERE signed_history.revision = current.source_revision
                        AND signed_history.digest_sha256 = current.source_digest_sha256
                  )
              )
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
    WHERE internal_rpc_authority.authority_snapshot_watermarks.source_revision <= EXCLUDED.source_revision
      AND internal_rpc_authority.authority_snapshot_watermarks.key_set_revision <= EXCLUDED.key_set_revision
      AND internal_rpc_authority.authority_snapshot_watermarks.policy_revision <= EXCLUDED.policy_revision
      AND internal_rpc_authority.authority_snapshot_watermarks.signer_generation <= EXCLUDED.signer_generation
      AND (
          (
              internal_rpc_authority.authority_snapshot_watermarks.source_revision < EXCLUDED.source_revision
              AND EXISTS (
                  SELECT 1
                  FROM unnest(
                      @history_revisions::bigint[],
                      @history_digests::text[]
                  ) AS signed_history(revision, digest_sha256)
                  WHERE signed_history.revision =
                      internal_rpc_authority.authority_snapshot_watermarks.source_revision
                    AND signed_history.digest_sha256 =
                      internal_rpc_authority.authority_snapshot_watermarks.source_digest_sha256
              )
          )
          OR internal_rpc_authority.authority_snapshot_watermarks.source_digest_sha256 = EXCLUDED.source_digest_sha256
      )
    RETURNING true
),
reserved AS (
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
    FROM accepted_snapshot
    ON CONFLICT (target_workload_id, jti) DO NOTHING
    RETURNING true
)
SELECT
    EXISTS (SELECT 1 FROM accepted_snapshot),
    EXISTS (SELECT 1 FROM reserved);
