(
    SELECT count(*)
    FROM internal_rpc_authority.authority_rotation_operations
    WHERE status <> 'RETIRED'
) + (
    SELECT count(*)
    FROM internal_rpc_authority.authority_rotation_intents AS intent
    WHERE intent.status NOT IN ('RETIRED', 'ABORTED')
      AND NOT COALESCE((
        -- У завершённой normal rotation последняя RETIRE публикация остаётся
        -- PROMOTED до prepare следующей. Принимаем только точную замкнутую связь
        -- завершающих receipts, а не произвольный PROMOTED или elapsed overlap.
        intent.status = 'PROMOTED'
        AND intent.protocol_version = 2
        AND intent.overlap_until <= clock_timestamp()
        AND EXISTS (
            SELECT 1
            FROM internal_rpc_authority.authority_snapshot_history AS head
            JOIN internal_rpc_authority.authority_rotation_operation_publications AS publication
              ON publication.publication_intent_id = head.publication_intent_id
             AND publication.source_revision = head.source_revision
             AND publication.snapshot_digest_sha256 = head.source_digest_sha256
             AND publication.publication_input_digest_sha256 = head.publication_input_digest_sha256
            JOIN internal_rpc_authority.authority_rotation_operation_phase_intents AS phase
              ON phase.operation_id = publication.operation_id
             AND phase.phase = publication.phase
             AND phase.publication_intent_id = publication.publication_intent_id
             AND phase.source_revision = publication.source_revision
             AND phase.registry_digest_sha256 = publication.registry_digest_sha256
             AND phase.publication_input_digest_sha256 = publication.publication_input_digest_sha256
            JOIN internal_rpc_authority.authority_rotation_operations AS operation
              ON operation.operation_id = publication.operation_id
             AND operation.registry_digest_sha256 = publication.registry_digest_sha256
            WHERE publication.phase = 'RETIRE'
              AND operation.status = 'RETIRED'
              AND operation.base_revision + 3 = head.source_revision
              AND operation.previous_not_after <= operation.completed_at
              AND publication.created_at <= operation.completed_at
              AND operation.completed_at <= clock_timestamp()
              AND operation.expected_readback_count = head.expected_readback_count
              AND intent.intent_id = head.publication_intent_id
              AND intent.source_revision = head.source_revision
              AND intent.source_digest_sha256 = head.source_digest_sha256
              AND intent.registry_source_digest_sha256 = publication.registry_digest_sha256
              AND head.source_revision = (
                SELECT max(source_revision) FROM internal_rpc_authority.authority_snapshot_history
              )
        )
      ), false)
)
