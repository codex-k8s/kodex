-- name: verifier__accept_context :one
WITH issued_binding AS MATERIALIZED (
 SELECT internal_rpc_authority.validate_issued_context_binding(
     @jti, @canonical_digest_sha256, @caller_workload_id, @target_workload_id,
     @source_revision, @source_digest_sha256, @key_set_revision, @policy_revision, @context_signer_generation
 ) AS accepted
), exact_snapshot AS MATERIALIZED (
    SELECT true AS accepted, @attestation_receipt_id::uuid AS receipt_id
    FROM internal_rpc_authority.authority_snapshot_watermarks AS current
    WHERE (SELECT accepted FROM issued_binding)
      AND current.target_workload_id = @target_workload_id
      AND current.source_revision = @source_revision
      AND current.source_digest_sha256 = @source_digest_sha256
      AND current.key_set_revision = @key_set_revision
      AND current.policy_revision = @policy_revision
      AND current.signer_generation = @signer_generation
      AND internal_rpc_authority.runtime_restore_fence_allows_work()
      AND internal_rpc_authority.snapshot_attestation_freshness_deadline(
          @attestation_receipt_id, @target_workload_id, @source_revision, @source_digest_sha256
      ) IS NOT NULL
      AND internal_rpc_authority.snapshot_attestation_freshness_deadline(
          current.readback_attestation_receipt_id,
          @target_workload_id,
          @source_revision,
          @source_digest_sha256
      ) IS NOT NULL
),
advanced_snapshot AS (
    INSERT INTO internal_rpc_authority.authority_snapshot_watermarks (
        target_workload_id,
        source_revision,
        source_digest_sha256,
        key_set_revision,
        policy_revision,
        signer_generation,
        readback_attestation_receipt_id,
        served_at
    )
    SELECT
        @target_workload_id,
        @source_revision,
        @source_digest_sha256,
        @key_set_revision,
        @policy_revision,
        @signer_generation,
        @attestation_receipt_id,
        clock_timestamp()
    WHERE (SELECT accepted FROM issued_binding)
      AND NOT EXISTS (SELECT 1 FROM exact_snapshot)
      AND internal_rpc_authority.runtime_restore_fence_allows_work()
      AND internal_rpc_authority.snapshot_attestation_freshness_deadline(
          @attestation_receipt_id,
          @target_workload_id,
          @source_revision,
          @source_digest_sha256
      ) IS NOT NULL
      AND (
          (
              NOT EXISTS (
                  SELECT 1
                  FROM internal_rpc_authority.authority_snapshot_watermarks AS initial
                  WHERE initial.target_workload_id = @target_workload_id
              )
              AND (
                  (
                      @source_revision = 1
                      AND @predecessor_revision = 0
                      AND @predecessor_digest_sha256 =
                          '0000000000000000000000000000000000000000000000000000000000000000'
                  )
                  OR (
                      @source_revision > 1
                      AND @predecessor_revision = @source_revision - 1
                      AND EXISTS (
                          SELECT 1
                          FROM unnest(
                              @history_revisions::bigint[],
                              @history_digests::text[]
                          ) AS signed_predecessor(revision, digest_sha256)
                          WHERE signed_predecessor.revision = @predecessor_revision
                            AND signed_predecessor.digest_sha256 =
                                @predecessor_digest_sha256
                      )
                  )
              )
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
        readback_attestation_receipt_id =
            EXCLUDED.readback_attestation_receipt_id,
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
      AND (
          internal_rpc_authority.authority_snapshot_watermarks.source_revision < EXCLUDED.source_revision
          OR internal_rpc_authority.authority_snapshot_watermarks.key_set_revision < EXCLUDED.key_set_revision
          OR internal_rpc_authority.authority_snapshot_watermarks.policy_revision < EXCLUDED.policy_revision
          OR internal_rpc_authority.authority_snapshot_watermarks.signer_generation < EXCLUDED.signer_generation
          OR internal_rpc_authority.authority_snapshot_watermarks.readback_attestation_receipt_id
              IS DISTINCT FROM EXCLUDED.readback_attestation_receipt_id
      )
    RETURNING true AS accepted, readback_attestation_receipt_id AS receipt_id
),
accepted_snapshot AS (
    SELECT accepted, receipt_id FROM exact_snapshot
    UNION ALL
    SELECT accepted, receipt_id FROM advanced_snapshot
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
    RETURNING internal_rpc_authority.validate_issued_context_binding(
        @jti, @canonical_digest_sha256, @caller_workload_id, @target_workload_id,
        @source_revision, @source_digest_sha256, @key_set_revision, @policy_revision, @context_signer_generation
    ) AND internal_rpc_authority.snapshot_attestation_freshness_deadline(
        (SELECT receipt_id FROM accepted_snapshot LIMIT 1),
        @target_workload_id, @source_revision, @source_digest_sha256
    ) IS NOT NULL AS still_valid
)
SELECT
    EXISTS (SELECT 1 FROM accepted_snapshot) AND NOT EXISTS (SELECT 1 FROM reserved WHERE NOT still_valid),
    EXISTS (SELECT 1 FROM reserved WHERE still_valid);
