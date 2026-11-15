-- +goose Up
SET ROLE internal_rpc_authority_readback_owner;

-- Закрытый successor единственного опубликованного predecessor. Владелец,
-- signature, OID и ACL сохраняются CREATE OR REPLACE; данные не изменяются.
-- +goose StatementBegin
DO $guard$
BEGIN
    IF current_database() <> 'internal_rpc_authority'
       OR NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_proc AS procedure
        JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = procedure.pronamespace
        JOIN pg_catalog.pg_roles AS owner ON owner.oid = procedure.proowner
        WHERE namespace.nspname = 'internal_rpc_authority'
          AND procedure.proname = 'authority_rotation_status'
          AND procedure.pronargs = 0
          AND procedure.prorettype = 'pg_catalog.jsonb'::pg_catalog.regtype
          AND owner.rolname = 'internal_rpc_authority_readback_owner'
          AND procedure.prosecdef AND procedure.provolatile = 'v'
          AND (SELECT count(*) = 2 AND bool_and(
              acl.privilege_type = 'EXECUTE' AND NOT acl.is_grantable
              AND acl.grantor = procedure.proowner
              AND acl.grantee IN (procedure.proowner,
                  'internal_rpc_authority_migrator'::pg_catalog.regrole))
              FROM pg_catalog.aclexplode(COALESCE(procedure.proacl,
                  pg_catalog.acldefault('f', procedure.proowner))) AS acl)
          AND pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(
              pg_catalog.pg_get_functiondef(procedure.oid), 'UTF8')), 'hex') =
              'c615a3d183ca67cadff5cf027d12adb7207bf9a4cf456dea02affe6f63b86af1'
       ) THEN
        RAISE EXCEPTION 'authority distribute status predecessor rejected';
    END IF;
END
$guard$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION internal_rpc_authority.authority_rotation_status()
RETURNS jsonb
    LANGUAGE sql VOLATILE SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
    SELECT CASE
        WHEN NOT pg_catalog.pg_has_role(
            session_user,
            'internal_rpc_authority_migrator',
            'MEMBER'
        ) THEN NULL
        WHEN operation.operation_id IS NOT NULL THEN
            pg_catalog.jsonb_strip_nulls(pg_catalog.jsonb_build_object(
                'observedAt', pg_catalog.clock_timestamp(),
                'operationId', operation.operation_id,
                'operationStatus', operation.status,
                'status', operation.status,
                'registryRevision', operation.registry_revision,
                'registrySourceDigestSHA256', operation.registry_digest_sha256,
                'baseRevision', operation.base_revision,
                'baseDigestSHA256', operation.base_digest_sha256,
                'expectedReadbackCount', operation.expected_readback_count,
                'switchNotBefore', operation.switch_not_before,
                'previousNotAfter', operation.previous_not_after,
                'completedAt', operation.completed_at,
                'phase', phase.phase,
                'intentId', intent.intent_id,
                'protocolVersion', intent.protocol_version,
                'sourceRevision', COALESCE(publication.source_revision, delivered.source_revision),
                'sourceDigestSHA256', COALESCE(publication.snapshot_digest_sha256, delivered.source_digest_sha256),
                'predecessorRevision', intent.predecessor_revision,
                'predecessorDigestSHA256', intent.predecessor_digest_sha256,
                'deliveryStartedAt', intent.delivery_started_at,
                'deliveredAt', intent.delivered_at,
                'promotedAt', intent.promoted_at,
                'overlapUntil', intent.overlap_until,
                'retiredAt', intent.retired_at,
                'abortedAt', intent.aborted_at,
                'actualReadbackCount', (
                    SELECT pg_catalog.count(*)
                      FROM internal_rpc_authority.authority_snapshot_readbacks AS readback
                     WHERE readback.source_revision = COALESCE(publication.source_revision, delivered.source_revision)
                       AND readback.digest_sha256 = COALESCE(publication.snapshot_digest_sha256, delivered.source_digest_sha256)
                )
            ))
        WHEN legacy.intent_id IS NULL THEN pg_catalog.jsonb_build_object(
            'observedAt', pg_catalog.clock_timestamp(),
            'status', 'EMPTY'
        )
        ELSE pg_catalog.jsonb_build_object(
            'observedAt', pg_catalog.clock_timestamp(),
            'intentId', legacy.intent_id,
            'protocolVersion', legacy.protocol_version,
            'sourceRevision', legacy.source_revision,
            'sourceDigestSHA256', legacy.source_digest_sha256,
            'registrySourceDigestSHA256', legacy.registry_source_digest_sha256,
            'predecessorRevision', legacy.predecessor_revision,
            'predecessorDigestSHA256', legacy.predecessor_digest_sha256,
            'status', legacy.status,
            'expectedReadbackCount', legacy.expected_readback_count,
            'actualReadbackCount', (
                SELECT pg_catalog.count(*)
                  FROM internal_rpc_authority.authority_snapshot_readbacks AS readback
                 WHERE readback.source_revision = legacy.source_revision
                   AND readback.digest_sha256 = legacy.source_digest_sha256
            ),
            'deliveryStartedAt', legacy.delivery_started_at,
            'deliveredAt', legacy.delivered_at,
            'promotedAt', legacy.promoted_at,
            'overlapUntil', legacy.overlap_until,
            'retiredAt', legacy.retired_at,
            'abortedAt', legacy.aborted_at
        )
    END
    FROM (SELECT 1) AS singleton
    LEFT JOIN LATERAL (
        SELECT * FROM internal_rpc_authority.authority_rotation_operations
         ORDER BY registry_revision DESC LIMIT 1
    ) AS operation ON true
    LEFT JOIN LATERAL (
        SELECT * FROM internal_rpc_authority.authority_rotation_operation_phase_intents
         WHERE operation_id=operation.operation_id
         ORDER BY source_revision DESC LIMIT 1
    ) AS phase ON true
    LEFT JOIN internal_rpc_authority.authority_rotation_operation_publications AS publication
      ON publication.operation_id=phase.operation_id AND publication.phase=phase.phase
    LEFT JOIN internal_rpc_authority.authority_rotation_intents AS intent
      ON intent.intent_id=phase.publication_intent_id
    LEFT JOIN LATERAL (
        -- DELIVERED history существует до readbacks и PROMOTED publication.
        -- Только полная provenance текущей DISTRIBUTE phase разрешает fallback.
        SELECT history.source_revision, history.source_digest_sha256
          FROM internal_rpc_authority.authority_snapshot_history AS history
         WHERE publication.operation_id IS NULL
           AND operation.status = 'DISTRIBUTING'
           AND phase.phase = 'DISTRIBUTE'
           AND phase.operation_id = operation.operation_id
           AND phase.source_revision = operation.base_revision + 1
           AND phase.registry_digest_sha256 = operation.registry_digest_sha256
           AND intent.intent_id = phase.publication_intent_id
           AND intent.protocol_version = 2
           AND intent.status = 'DELIVERED'
           AND intent.registry_source_digest_sha256 = operation.registry_digest_sha256
           AND intent.source_revision = phase.source_revision
           AND intent.expected_readback_count = operation.expected_readback_count
           AND intent.predecessor_revision = operation.base_revision
           AND intent.predecessor_digest_sha256 = operation.base_digest_sha256
           AND intent.delivery_started_at IS NOT NULL
           AND intent.delivered_at IS NOT NULL
           AND intent.delivery_started_at <= intent.delivered_at
           AND intent.promoted_at IS NULL
           AND intent.retired_at IS NULL
           AND intent.aborted_at IS NULL
           AND history.publication_intent_id = intent.intent_id
           AND history.source_revision = intent.source_revision
           AND history.source_digest_sha256 = intent.source_digest_sha256
           AND history.publication_input_digest_sha256 = phase.publication_input_digest_sha256
           AND history.expected_readback_count = operation.expected_readback_count
           AND history.predecessor_revision = operation.base_revision
           AND history.predecessor_digest_sha256 = operation.base_digest_sha256
           AND history.published_at <= intent.delivered_at
    ) AS delivered ON true
    LEFT JOIN LATERAL (
        SELECT * FROM internal_rpc_authority.authority_rotation_intents
         ORDER BY source_revision DESC, created_at DESC, intent_id DESC LIMIT 1
    ) AS legacy ON operation.operation_id IS NULL
$$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $guard$
BEGIN
    RAISE EXCEPTION 'authority distribute status migration is forward-only';
END
$guard$;
-- +goose StatementEnd
