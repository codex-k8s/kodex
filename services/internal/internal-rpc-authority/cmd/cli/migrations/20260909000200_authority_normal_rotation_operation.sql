-- +goose Up
SET ROLE internal_rpc_authority_readback_owner;

CREATE TABLE internal_rpc_authority.authority_rotation_operations (
    operation_id uuid PRIMARY KEY,
    registry_revision bigint NOT NULL UNIQUE
        CHECK (registry_revision BETWEEN 1 AND 9007199254740991),
    registry_digest_sha256 text NOT NULL
        CHECK (registry_digest_sha256 ~ '^[a-f0-9]{64}$'),
    base_revision bigint NOT NULL
        CHECK (base_revision BETWEEN 1 AND 9007199254740988),
    base_digest_sha256 text NOT NULL
        CHECK (base_digest_sha256 ~ '^[a-f0-9]{64}$'),
    expected_readback_count integer NOT NULL
        CHECK (expected_readback_count BETWEEN 1 AND 384),
    status text NOT NULL CHECK (status IN (
        'DISTRIBUTING', 'WAITING_SWITCH', 'SWITCHING',
        'WAITING_RETIRE', 'RETIRING', 'RETIRED'
    )),
    switch_not_before timestamp with time zone,
    previous_not_after timestamp with time zone,
    completed_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CHECK ((status NOT IN ('WAITING_SWITCH', 'SWITCHING', 'WAITING_RETIRE', 'RETIRING', 'RETIRED')) OR switch_not_before IS NOT NULL),
    CHECK ((status NOT IN ('SWITCHING', 'WAITING_RETIRE', 'RETIRING', 'RETIRED')) OR previous_not_after IS NOT NULL),
    CHECK ((status <> 'RETIRED') OR completed_at IS NOT NULL)
);

CREATE UNIQUE INDEX authority_rotation_operations_active_idx
    ON internal_rpc_authority.authority_rotation_operations ((true))
    WHERE status <> 'RETIRED';

CREATE TABLE internal_rpc_authority.authority_rotation_operation_phase_intents (
    operation_id uuid NOT NULL REFERENCES internal_rpc_authority.authority_rotation_operations(operation_id),
    phase text NOT NULL CHECK (phase IN ('DISTRIBUTE', 'SWITCH', 'RETIRE')),
    publication_intent_id uuid NOT NULL UNIQUE REFERENCES internal_rpc_authority.authority_rotation_intents(intent_id),
    source_revision bigint NOT NULL UNIQUE,
    registry_digest_sha256 text NOT NULL CHECK (registry_digest_sha256 ~ '^[a-f0-9]{64}$'),
    publication_input_digest_sha256 text NOT NULL CHECK (publication_input_digest_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamp with time zone NOT NULL,
    PRIMARY KEY (operation_id, phase)
);

CREATE TABLE internal_rpc_authority.authority_rotation_operation_publications (
    operation_id uuid NOT NULL,
    phase text NOT NULL,
    publication_intent_id uuid NOT NULL UNIQUE,
    source_revision bigint NOT NULL UNIQUE,
    registry_digest_sha256 text NOT NULL CHECK (registry_digest_sha256 ~ '^[a-f0-9]{64}$'),
    publication_input_digest_sha256 text NOT NULL CHECK (publication_input_digest_sha256 ~ '^[a-f0-9]{64}$'),
    snapshot_digest_sha256 text NOT NULL CHECK (snapshot_digest_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamp with time zone NOT NULL,
    PRIMARY KEY (operation_id, phase),
    FOREIGN KEY (operation_id, phase)
        REFERENCES internal_rpc_authority.authority_rotation_operation_phase_intents(operation_id, phase),
    FOREIGN KEY (publication_intent_id)
        REFERENCES internal_rpc_authority.authority_rotation_intents(intent_id)
);

ALTER TABLE internal_rpc_authority.authority_rotation_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_rotation_operations FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_rotation_operation_phase_intents ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_rotation_operation_phase_intents FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_rotation_operation_publications ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_rotation_operation_publications FORCE ROW LEVEL SECURITY;
CREATE POLICY authority_rotation_operations_owner ON internal_rpc_authority.authority_rotation_operations
    TO internal_rpc_authority_readback_owner USING (true) WITH CHECK (true);
CREATE POLICY authority_rotation_operation_phase_intents_owner ON internal_rpc_authority.authority_rotation_operation_phase_intents
    TO internal_rpc_authority_readback_owner USING (true) WITH CHECK (true);
CREATE POLICY authority_rotation_operation_publications_owner ON internal_rpc_authority.authority_rotation_operation_publications
    TO internal_rpc_authority_readback_owner USING (true) WITH CHECK (true);

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.publisher_load_rotation_operation(
    p_operation_id uuid
) RETURNS SETOF internal_rpc_authority.authority_rotation_operations
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
DECLARE
    existing internal_rpc_authority.authority_rotation_operations%ROWTYPE;
BEGIN
    IF NOT pg_catalog.pg_has_role(session_user, 'internal_rpc_authority_publisher', 'MEMBER') THEN
        RETURN;
    END IF;
    SELECT * INTO existing
      FROM internal_rpc_authority.authority_rotation_operations
     WHERE operation_id = p_operation_id;
    IF FOUND THEN
        RETURN NEXT existing;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.publisher_load_or_prepare_rotation_operation(
    p_operation_id uuid, p_registry_revision bigint, p_registry_digest_sha256 text,
    p_base_revision bigint, p_base_digest_sha256 text, p_expected_readback_count integer
) RETURNS SETOF internal_rpc_authority.authority_rotation_operations
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
DECLARE
    existing internal_rpc_authority.authority_rotation_operations%ROWTYPE;
    latest internal_rpc_authority.authority_snapshot_history%ROWTYPE;
    prior_registry_revision bigint;
BEGIN
    IF NOT pg_catalog.pg_has_role(session_user, 'internal_rpc_authority_publisher', 'MEMBER')
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
       OR p_registry_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_registry_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_base_revision NOT BETWEEN 1 AND 9007199254740988
       OR p_base_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_expected_readback_count NOT BETWEEN 1 AND 384 THEN
        RETURN;
    END IF;
    PERFORM pg_catalog.pg_advisory_xact_lock(1390, 1428);
    SELECT * INTO existing FROM internal_rpc_authority.authority_rotation_operations
     WHERE operation_id = p_operation_id FOR UPDATE;
    IF FOUND THEN
        IF existing.registry_revision <> p_registry_revision
           OR existing.registry_digest_sha256 <> p_registry_digest_sha256
           OR existing.expected_readback_count <> p_expected_readback_count THEN
            RETURN;
        END IF;
    ELSE
        IF EXISTS (SELECT 1 FROM internal_rpc_authority.authority_rotation_operations WHERE status <> 'RETIRED') THEN RETURN; END IF;
        SELECT * INTO latest FROM internal_rpc_authority.authority_snapshot_history ORDER BY source_revision DESC LIMIT 1 FOR UPDATE;
        IF NOT FOUND OR latest.source_revision <> p_base_revision OR latest.source_digest_sha256 <> p_base_digest_sha256 THEN RETURN; END IF;
        SELECT max(registry_revision) INTO prior_registry_revision FROM internal_rpc_authority.authority_rotation_operations;
        IF (prior_registry_revision IS NULL AND p_registry_revision <> p_base_revision + 1)
           OR (prior_registry_revision IS NOT NULL AND p_registry_revision <> prior_registry_revision + 1) THEN RETURN; END IF;
        INSERT INTO internal_rpc_authority.authority_rotation_operations
            (operation_id, registry_revision, registry_digest_sha256, base_revision,
             base_digest_sha256, expected_readback_count, status, created_at, updated_at)
        VALUES (p_operation_id, p_registry_revision, p_registry_digest_sha256, p_base_revision,
                p_base_digest_sha256, p_expected_readback_count, 'DISTRIBUTING', clock_timestamp(), clock_timestamp())
        RETURNING * INTO existing;
    END IF;
    IF existing.status = 'WAITING_SWITCH' AND existing.switch_not_before <= clock_timestamp() THEN
        UPDATE internal_rpc_authority.authority_rotation_operations
           SET status = 'SWITCHING', previous_not_after = COALESCE(previous_not_after, clock_timestamp() + interval '40 seconds'), updated_at = clock_timestamp()
         WHERE operation_id = existing.operation_id AND status = 'WAITING_SWITCH' RETURNING * INTO existing;
    ELSIF existing.status = 'WAITING_RETIRE' AND existing.previous_not_after <= clock_timestamp() THEN
        UPDATE internal_rpc_authority.authority_rotation_operations
           SET status = 'RETIRING', updated_at = clock_timestamp()
         WHERE operation_id = existing.operation_id AND status = 'WAITING_RETIRE' RETURNING * INTO existing;
    END IF;
    RETURN NEXT existing;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.publisher_prepare_rotation_phase(
    p_operation_id uuid, p_phase text, p_intent_id uuid, p_source_revision bigint,
    p_source_digest_sha256 text, p_predecessor_revision bigint,
    p_predecessor_digest_sha256 text, p_expected_readback_count integer,
    p_publication_input_digest_sha256 text
) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
DECLARE op internal_rpc_authority.authority_rotation_operations%ROWTYPE;
DECLARE phase_intent internal_rpc_authority.authority_rotation_operation_phase_intents%ROWTYPE;
DECLARE expected_status text;
DECLARE expected_revision bigint;
BEGIN
    IF NOT pg_catalog.pg_has_role(session_user, 'internal_rpc_authority_publisher', 'MEMBER')
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
    THEN
        RETURN false;
    END IF;
    SELECT * INTO op FROM internal_rpc_authority.authority_rotation_operations WHERE operation_id = p_operation_id FOR UPDATE;
    expected_status := CASE p_phase WHEN 'DISTRIBUTE' THEN 'DISTRIBUTING' WHEN 'SWITCH' THEN 'SWITCHING' WHEN 'RETIRE' THEN 'RETIRING' END;
    expected_revision := op.base_revision + CASE p_phase WHEN 'DISTRIBUTE' THEN 1 WHEN 'SWITCH' THEN 2 WHEN 'RETIRE' THEN 3 END;
    IF NOT FOUND OR expected_status IS NULL OR op.status <> expected_status
       OR p_source_revision <> expected_revision OR p_expected_readback_count <> op.expected_readback_count
       OR p_publication_input_digest_sha256 !~ '^[a-f0-9]{64}$' THEN RETURN false; END IF;
	SELECT * INTO phase_intent
	  FROM internal_rpc_authority.authority_rotation_operation_phase_intents
	 WHERE operation_id=p_operation_id AND phase=p_phase;
	IF FOUND THEN
		RETURN phase_intent.publication_intent_id=p_intent_id
		   AND phase_intent.source_revision=p_source_revision
		   AND phase_intent.registry_digest_sha256=op.registry_digest_sha256
		   AND phase_intent.publication_input_digest_sha256=p_publication_input_digest_sha256;
	END IF;
    IF NOT internal_rpc_authority.publisher_prepare_rotation(p_intent_id, p_source_revision,
        p_source_digest_sha256, p_predecessor_revision, p_predecessor_digest_sha256,
        p_expected_readback_count) THEN RETURN false; END IF;
    INSERT INTO internal_rpc_authority.authority_rotation_operation_phase_intents
        (operation_id, phase, publication_intent_id, source_revision,
         registry_digest_sha256, publication_input_digest_sha256, created_at)
    VALUES (p_operation_id, p_phase, p_intent_id, p_source_revision,
            op.registry_digest_sha256, p_publication_input_digest_sha256, clock_timestamp())
    ON CONFLICT (operation_id, phase) DO NOTHING;
    RETURN EXISTS (SELECT 1 FROM internal_rpc_authority.authority_rotation_operation_phase_intents
        WHERE operation_id=p_operation_id AND phase=p_phase AND publication_intent_id=p_intent_id
          AND source_revision=p_source_revision
          AND registry_digest_sha256=op.registry_digest_sha256
          AND publication_input_digest_sha256=p_publication_input_digest_sha256);
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.publisher_advance_rotation_operation(
    p_operation_id uuid, p_registry_revision bigint, p_registry_digest_sha256 text,
    p_base_revision bigint, p_base_digest_sha256 text, p_expected_readback_count integer,
    p_phase text, p_intent_id uuid, p_source_revision bigint,
    p_source_digest_sha256 text, p_publication_input_digest_sha256 text
) RETURNS SETOF internal_rpc_authority.authority_rotation_operations
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
DECLARE op internal_rpc_authority.authority_rotation_operations%ROWTYPE;
DECLARE intent internal_rpc_authority.authority_rotation_intents%ROWTYPE;
DECLARE op_found boolean;
DECLARE intent_found boolean;
BEGIN
    IF NOT pg_catalog.pg_has_role(session_user, 'internal_rpc_authority_publisher', 'MEMBER')
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
    THEN
        RETURN;
    END IF;
    SELECT * INTO op FROM internal_rpc_authority.authority_rotation_operations WHERE operation_id=p_operation_id FOR UPDATE;
    op_found := FOUND;
    SELECT * INTO intent FROM internal_rpc_authority.authority_rotation_intents WHERE intent_id=p_intent_id FOR UPDATE;
    intent_found := FOUND;
    IF NOT op_found OR NOT intent_found
       OR op.registry_revision<>p_registry_revision OR op.registry_digest_sha256<>p_registry_digest_sha256
       OR op.base_revision<>p_base_revision OR op.base_digest_sha256<>p_base_digest_sha256
       OR op.expected_readback_count<>p_expected_readback_count OR intent.status<>'PROMOTED'
       OR intent.source_revision<>p_source_revision OR intent.source_digest_sha256<>p_source_digest_sha256
       OR NOT EXISTS (SELECT 1 FROM internal_rpc_authority.authority_rotation_operation_phase_intents
          WHERE operation_id=p_operation_id AND phase=p_phase AND publication_intent_id=p_intent_id
            AND source_revision=p_source_revision
            AND registry_digest_sha256=p_registry_digest_sha256
            AND publication_input_digest_sha256=p_publication_input_digest_sha256) THEN RETURN; END IF;
    INSERT INTO internal_rpc_authority.authority_rotation_operation_publications
        (operation_id, phase, publication_intent_id, source_revision,
         registry_digest_sha256, publication_input_digest_sha256,
         snapshot_digest_sha256, created_at)
    VALUES (p_operation_id, p_phase, p_intent_id, p_source_revision,
            p_registry_digest_sha256, p_publication_input_digest_sha256,
            p_source_digest_sha256, clock_timestamp())
    ON CONFLICT (operation_id, phase) DO NOTHING;
    IF NOT EXISTS (
        SELECT 1 FROM internal_rpc_authority.authority_rotation_operation_publications
         WHERE operation_id=p_operation_id AND phase=p_phase
           AND publication_intent_id=p_intent_id
           AND source_revision=p_source_revision
           AND registry_digest_sha256=p_registry_digest_sha256
           AND publication_input_digest_sha256=p_publication_input_digest_sha256
           AND snapshot_digest_sha256=p_source_digest_sha256
    ) THEN RETURN; END IF;
    IF p_phase='DISTRIBUTE' AND op.status='DISTRIBUTING' THEN
        UPDATE internal_rpc_authority.authority_rotation_operations SET status='WAITING_SWITCH',
            switch_not_before=intent.overlap_until, updated_at=clock_timestamp() WHERE operation_id=p_operation_id RETURNING * INTO op;
    ELSIF p_phase='SWITCH' AND op.status='SWITCHING' THEN
        UPDATE internal_rpc_authority.authority_rotation_intents
           SET overlap_until=op.previous_not_after,
               updated_at=clock_timestamp()
         WHERE intent_id=p_intent_id
           AND status='PROMOTED';
        IF NOT FOUND THEN RETURN; END IF;
        UPDATE internal_rpc_authority.authority_rotation_operations SET status='WAITING_RETIRE', updated_at=clock_timestamp()
            WHERE operation_id=p_operation_id RETURNING * INTO op;
    ELSIF p_phase='RETIRE' AND op.status='RETIRING' THEN
        UPDATE internal_rpc_authority.authority_rotation_operations SET status='RETIRED', completed_at=clock_timestamp(), updated_at=clock_timestamp()
            WHERE operation_id=p_operation_id RETURNING * INTO op;
    END IF;
    RETURN NEXT op;
END
$$;
-- +goose StatementEnd

-- Операторский readback показывает составную operation и последнюю immutable
-- phase publication, не раскрывая JWS или key material.
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
                'sourceRevision', publication.source_revision,
                'sourceDigestSHA256', publication.snapshot_digest_sha256,
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
                     WHERE readback.source_revision = publication.source_revision
                       AND readback.digest_sha256 = publication.snapshot_digest_sha256
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
        SELECT * FROM internal_rpc_authority.authority_rotation_intents
         ORDER BY source_revision DESC, created_at DESC, intent_id DESC LIMIT 1
    ) AS legacy ON operation.operation_id IS NULL
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION internal_rpc_authority.publisher_load_rotation_operation(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION internal_rpc_authority.publisher_load_or_prepare_rotation_operation(uuid,bigint,text,bigint,text,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION internal_rpc_authority.publisher_prepare_rotation_phase(uuid,text,uuid,bigint,text,bigint,text,integer,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION internal_rpc_authority.publisher_advance_rotation_operation(uuid,bigint,text,bigint,text,integer,text,uuid,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_load_rotation_operation(uuid) TO internal_rpc_authority_publisher;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_load_or_prepare_rotation_operation(uuid,bigint,text,bigint,text,integer) TO internal_rpc_authority_publisher;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_prepare_rotation_phase(uuid,text,uuid,bigint,text,bigint,text,integer,text) TO internal_rpc_authority_publisher;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_advance_rotation_operation(uuid,bigint,text,bigint,text,integer,text,uuid,bigint,text,text) TO internal_rpc_authority_publisher;

RESET ROLE;

-- +goose Down
SELECT 1;
