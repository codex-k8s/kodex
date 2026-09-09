-- +goose Up
SET ROLE internal_rpc_authority_readback_owner;

-- Нормальная ротация authority использует прежнюю таблицу намерений как
-- forward-only owner boundary. protocol_version=1 сохраняет совместимость
-- старого publisher на expand-фазе; новый writer создаёт только version=2.
ALTER TABLE internal_rpc_authority.authority_rotation_intents
    DROP CONSTRAINT authority_rotation_intents_status_check;

ALTER TABLE internal_rpc_authority.authority_rotation_intents
    ADD COLUMN protocol_version integer NOT NULL DEFAULT 1,
    ADD COLUMN registry_source_digest_sha256 text,
    ADD COLUMN predecessor_revision bigint,
    ADD COLUMN predecessor_digest_sha256 text,
    ADD COLUMN expected_readback_count integer,
    ADD COLUMN delivery_started_at timestamp with time zone,
    ADD COLUMN delivered_at timestamp with time zone,
    ADD COLUMN promoted_at timestamp with time zone,
    ADD COLUMN overlap_until timestamp with time zone,
    ADD COLUMN retired_at timestamp with time zone,
    ADD COLUMN aborted_at timestamp with time zone;

UPDATE internal_rpc_authority.authority_rotation_intents
SET registry_source_digest_sha256 = source_digest_sha256,
    delivery_started_at = CASE
        WHEN status IN ('DELIVERED', 'PROMOTED') THEN updated_at
        ELSE NULL
    END,
    delivered_at = CASE
        WHEN status IN ('DELIVERED', 'PROMOTED') THEN updated_at
        ELSE NULL
    END,
    promoted_at = CASE WHEN status = 'PROMOTED' THEN updated_at ELSE NULL END,
    overlap_until = CASE
        WHEN status = 'PROMOTED' THEN updated_at + interval '40 seconds'
        ELSE NULL
    END;

ALTER TABLE internal_rpc_authority.authority_rotation_intents
    ALTER COLUMN registry_source_digest_sha256 SET NOT NULL,
    ADD CONSTRAINT authority_rotation_intents_status_check
        CHECK (status IN (
            'PREPARED', 'DELIVERING', 'DELIVERED',
            'PROMOTED', 'RETIRED', 'ABORTED'
        )),
    ADD CONSTRAINT authority_rotation_intents_protocol_version_check
        CHECK (protocol_version IN (1, 2)),
    ADD CONSTRAINT authority_rotation_intents_registry_source_digest_check
        CHECK (registry_source_digest_sha256 ~ '^[a-f0-9]{64}$'),
    ADD CONSTRAINT authority_rotation_intents_predecessor_revision_check
        CHECK (predecessor_revision IS NULL OR predecessor_revision >= 0),
    ADD CONSTRAINT authority_rotation_intents_predecessor_digest_check
        CHECK (
            predecessor_digest_sha256 IS NULL
            OR predecessor_digest_sha256 ~ '^[a-f0-9]{64}$'
        ),
    ADD CONSTRAINT authority_rotation_intents_expected_readback_count_check
        CHECK (
            expected_readback_count IS NULL
            OR expected_readback_count BETWEEN 1 AND 384
        ),
    ADD CONSTRAINT authority_rotation_intents_v2_shape_check
        CHECK (
            protocol_version = 1
            OR (
                predecessor_revision IS NOT NULL
                AND predecessor_digest_sha256 IS NOT NULL
                AND expected_readback_count IS NOT NULL
            )
        ),
    ADD CONSTRAINT authority_rotation_intents_terminal_timestamps_check
        CHECK (
            (status <> 'ABORTED' OR aborted_at IS NOT NULL)
            AND (
                status NOT IN ('DELIVERING', 'DELIVERED', 'PROMOTED', 'RETIRED')
                OR delivery_started_at IS NOT NULL
            )
            AND (status <> 'RETIRED' OR retired_at IS NOT NULL)
            AND (status NOT IN ('DELIVERED', 'PROMOTED', 'RETIRED') OR delivered_at IS NOT NULL)
            AND (status NOT IN ('PROMOTED', 'RETIRED') OR promoted_at IS NOT NULL)
            AND (status NOT IN ('PROMOTED', 'RETIRED') OR overlap_until IS NOT NULL)
        );

CREATE UNIQUE INDEX authority_rotation_intents_active_v2_idx
    ON internal_rpc_authority.authority_rotation_intents ((protocol_version))
    WHERE protocol_version = 2
      AND status IN ('PREPARED', 'DELIVERING', 'DELIVERED');

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.publisher_prepare_rotation(
    p_intent_id uuid,
    p_source_revision bigint,
    p_source_digest_sha256 text,
    p_predecessor_revision bigint,
    p_predecessor_digest_sha256 text,
    p_expected_readback_count integer
) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
DECLARE
    existing internal_rpc_authority.authority_rotation_intents%ROWTYPE;
    latest internal_rpc_authority.authority_snapshot_history%ROWTYPE;
    predecessor internal_rpc_authority.authority_rotation_intents%ROWTYPE;
    zero_digest constant text := repeat('0', 64);
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_publisher',
        'MEMBER'
    )
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
       OR p_source_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_source_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_predecessor_revision NOT BETWEEN 0 AND 9007199254740991
       OR p_predecessor_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_expected_readback_count NOT BETWEEN 1 AND 384
    THEN
        RETURN false;
    END IF;

    SELECT * INTO existing
    FROM internal_rpc_authority.authority_rotation_intents
    WHERE intent_id = p_intent_id
    FOR UPDATE;
    IF FOUND THEN
        RETURN existing.protocol_version = 2
           AND existing.source_revision = p_source_revision
           AND existing.registry_source_digest_sha256 = p_source_digest_sha256
           AND existing.predecessor_revision = p_predecessor_revision
           AND existing.predecessor_digest_sha256 = p_predecessor_digest_sha256
           AND existing.expected_readback_count = p_expected_readback_count
           AND existing.status IN (
                'PREPARED', 'DELIVERING', 'DELIVERED', 'PROMOTED', 'RETIRED'
           );
    END IF;

    IF EXISTS (
        SELECT 1
        FROM internal_rpc_authority.authority_rotation_intents
        WHERE (source_revision = p_source_revision AND status <> 'ABORTED')
           OR (protocol_version = 2
               AND status IN ('PREPARED', 'DELIVERING', 'DELIVERED'))
    ) THEN
        RETURN false;
    END IF;

    SELECT * INTO latest
    FROM internal_rpc_authority.authority_snapshot_history
    ORDER BY source_revision DESC
    LIMIT 1
    FOR UPDATE;
    IF NOT FOUND THEN
        IF p_source_revision <> 1
           OR p_predecessor_revision <> 0
           OR p_predecessor_digest_sha256 <> zero_digest
        THEN
            RETURN false;
        END IF;
    ELSE
        IF p_source_revision <> latest.source_revision + 1
           OR p_predecessor_revision <> latest.source_revision
           OR p_predecessor_digest_sha256 <> latest.source_digest_sha256
        THEN
            RETURN false;
        END IF;
        SELECT * INTO predecessor
        FROM internal_rpc_authority.authority_rotation_intents
        WHERE intent_id = latest.publication_intent_id
          AND source_revision = latest.source_revision
          AND source_digest_sha256 = latest.source_digest_sha256
        FOR UPDATE;
        IF NOT FOUND
           OR predecessor.status NOT IN ('PROMOTED', 'RETIRED')
           OR predecessor.overlap_until IS NULL
           OR predecessor.overlap_until > pg_catalog.clock_timestamp()
        THEN
            RETURN false;
        END IF;
        IF predecessor.status = 'PROMOTED' THEN
            UPDATE internal_rpc_authority.authority_rotation_intents
            SET status = 'RETIRED',
                retired_at = pg_catalog.clock_timestamp(),
                updated_at = pg_catalog.clock_timestamp()
            WHERE intent_id = predecessor.intent_id
              AND status = 'PROMOTED';
            IF NOT FOUND THEN
                RETURN false;
            END IF;
        END IF;
    END IF;

    INSERT INTO internal_rpc_authority.authority_rotation_intents (
        intent_id,
        source_revision,
        source_digest_sha256,
        registry_source_digest_sha256,
        status,
        protocol_version,
        predecessor_revision,
        predecessor_digest_sha256,
        expected_readback_count,
        created_at,
        updated_at
    ) VALUES (
        p_intent_id,
        p_source_revision,
        p_source_digest_sha256,
        p_source_digest_sha256,
        'PREPARED',
        2,
        p_predecessor_revision,
        p_predecessor_digest_sha256,
        p_expected_readback_count,
        pg_catalog.clock_timestamp(),
        pg_catalog.clock_timestamp()
    );
    RETURN true;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.publisher_begin_rotation_delivery(
    p_intent_id uuid,
    p_source_revision bigint,
    p_source_digest_sha256 text
) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
DECLARE
    current_status text;
    current_protocol integer;
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_publisher',
        'MEMBER'
    )
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
       OR p_source_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_source_digest_sha256 !~ '^[a-f0-9]{64}$'
    THEN
        RETURN false;
    END IF;
    SELECT status, protocol_version
      INTO current_status, current_protocol
      FROM internal_rpc_authority.authority_rotation_intents
     WHERE intent_id = p_intent_id
       AND source_revision = p_source_revision
       AND (
            source_digest_sha256 = p_source_digest_sha256
            OR registry_source_digest_sha256 = p_source_digest_sha256
       )
     FOR UPDATE;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    IF current_protocol = 1 THEN
        RETURN current_status IN ('PREPARED', 'DELIVERED', 'PROMOTED', 'RETIRED');
    END IF;
    IF current_status = 'PREPARED' THEN
        UPDATE internal_rpc_authority.authority_rotation_intents
           SET status = 'DELIVERING',
               delivery_started_at = pg_catalog.clock_timestamp(),
               updated_at = pg_catalog.clock_timestamp()
         WHERE intent_id = p_intent_id
           AND status = 'PREPARED';
        RETURN FOUND;
    END IF;
    RETURN current_status IN ('DELIVERING', 'DELIVERED', 'PROMOTED', 'RETIRED');
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.publisher_mark_rotation_delivered(
    p_intent_id uuid,
    p_source_revision bigint,
    p_source_digest_sha256 text
) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
DECLARE
    current_status text;
    current_protocol integer;
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_publisher',
        'MEMBER'
    )
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
    THEN
        RETURN false;
    END IF;
    SELECT status, protocol_version
      INTO current_status, current_protocol
      FROM internal_rpc_authority.authority_rotation_intents
     WHERE intent_id = p_intent_id
       AND source_revision = p_source_revision
       AND source_digest_sha256 = p_source_digest_sha256
     FOR UPDATE;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    IF current_protocol = 1 THEN
        RETURN current_status IN ('PREPARED', 'DELIVERED', 'PROMOTED', 'RETIRED');
    END IF;
    IF current_status = 'DELIVERING'
       AND EXISTS (
            SELECT 1
            FROM internal_rpc_authority.authority_snapshot_history
            WHERE publication_intent_id = p_intent_id
              AND source_revision = p_source_revision
              AND source_digest_sha256 = p_source_digest_sha256
       )
    THEN
        UPDATE internal_rpc_authority.authority_rotation_intents
           SET status = 'DELIVERED',
               delivered_at = pg_catalog.clock_timestamp(),
               updated_at = pg_catalog.clock_timestamp()
         WHERE intent_id = p_intent_id
           AND status = 'DELIVERING';
        RETURN FOUND;
    END IF;
    RETURN current_status IN ('DELIVERED', 'PROMOTED', 'RETIRED');
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.publisher_abort_rotation(
    p_intent_id uuid,
    p_source_revision bigint,
    p_source_digest_sha256 text
) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
BEGIN
    IF NOT (
        pg_catalog.pg_has_role(
            session_user,
            'internal_rpc_authority_publisher',
            'MEMBER'
        )
        OR pg_catalog.pg_has_role(
            session_user,
            'internal_rpc_authority_migrator',
            'MEMBER'
        )
    )
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
    THEN
        RETURN false;
    END IF;
    UPDATE internal_rpc_authority.authority_rotation_intents AS intent
       SET status = 'ABORTED',
           aborted_at = pg_catalog.clock_timestamp(),
           updated_at = pg_catalog.clock_timestamp()
     WHERE intent.intent_id = p_intent_id
       AND intent.source_revision = p_source_revision
       AND (
            intent.source_digest_sha256 = p_source_digest_sha256
            OR intent.registry_source_digest_sha256 = p_source_digest_sha256
       )
       AND intent.protocol_version = 2
       AND intent.status = 'PREPARED'
       AND intent.delivery_started_at IS NULL
       AND NOT EXISTS (
            SELECT 1
            FROM internal_rpc_authority.authority_snapshot_history AS history
            WHERE history.publication_intent_id = intent.intent_id
       );
    RETURN FOUND;
END
$$;
-- +goose StatementEnd

-- Операторский readback не раскрывает JWS, ключи или credential material.
-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.authority_rotation_status()
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
        WHEN intent.intent_id IS NULL THEN pg_catalog.jsonb_build_object(
            'observedAt', pg_catalog.clock_timestamp(),
            'status', 'EMPTY'
        )
        ELSE pg_catalog.jsonb_build_object(
            'observedAt', pg_catalog.clock_timestamp(),
            'intentId', intent.intent_id,
            'protocolVersion', intent.protocol_version,
            'sourceRevision', intent.source_revision,
            'sourceDigestSHA256', intent.source_digest_sha256,
            'registrySourceDigestSHA256', intent.registry_source_digest_sha256,
            'predecessorRevision', intent.predecessor_revision,
            'predecessorDigestSHA256', intent.predecessor_digest_sha256,
            'status', intent.status,
            'expectedReadbackCount', intent.expected_readback_count,
            'actualReadbackCount', (
                SELECT pg_catalog.count(*)
                FROM internal_rpc_authority.authority_snapshot_readbacks AS readback
                WHERE readback.source_revision = intent.source_revision
                  AND readback.digest_sha256 = intent.source_digest_sha256
            ),
            'deliveryStartedAt', intent.delivery_started_at,
            'deliveredAt', intent.delivered_at,
            'promotedAt', intent.promoted_at,
            'overlapUntil', intent.overlap_until,
            'retiredAt', intent.retired_at,
            'abortedAt', intent.aborted_at
        )
    END
    FROM (SELECT 1) AS singleton
    LEFT JOIN LATERAL (
        SELECT *
        FROM internal_rpc_authority.authority_rotation_intents
        ORDER BY source_revision DESC, created_at DESC, intent_id DESC
        LIMIT 1
    ) AS intent ON true
$$;
-- +goose StatementEnd

-- Старый writer остаётся допустимым только как protocol_version=1. Новый
-- writer заранее создаёт protocol_version=2 intent и требует DELIVERING.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION internal_rpc_authority.publisher_append_snapshot_history(
    p_source_revision bigint,
    p_source_digest_sha256 text,
    p_key_set_revision bigint,
    p_policy_revision bigint,
    p_signer_generation bigint,
    p_predecessor_revision bigint,
    p_predecessor_digest_sha256 text,
    p_snapshot_compact_jws text,
    p_publication_intent_id uuid,
    p_publication_input_digest_sha256 text,
    p_expected_readback_count integer,
    p_published_at timestamp with time zone
) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
DECLARE
    latest internal_rpc_authority.authority_snapshot_history%ROWTYPE;
    intent internal_rpc_authority.authority_rotation_intents%ROWTYPE;
    zero_digest constant text := repeat('0', 64);
BEGIN
    IF NOT pg_catalog.pg_has_role(session_user, 'internal_rpc_authority_publisher', 'MEMBER')
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
       OR p_source_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_source_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_publication_input_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_predecessor_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_key_set_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_policy_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_signer_generation NOT BETWEEN 1 AND 9007199254740991
       OR p_expected_readback_count NOT BETWEEN 1 AND 384
       OR p_snapshot_compact_jws IS NULL
       OR length(p_snapshot_compact_jws) NOT BETWEEN 32 AND 1048576
       OR p_published_at IS NULL
    THEN
        RETURN false;
    END IF;
    SELECT * INTO latest
      FROM internal_rpc_authority.authority_snapshot_history
     ORDER BY source_revision DESC
     LIMIT 1
     FOR UPDATE;
    IF p_source_revision = 1 THEN
        IF FOUND OR p_predecessor_revision <> 0 OR p_predecessor_digest_sha256 <> zero_digest THEN
            RETURN false;
        END IF;
    ELSIF NOT FOUND
       OR p_source_revision <> latest.source_revision + 1
       OR p_predecessor_revision <> latest.source_revision
       OR p_predecessor_digest_sha256 <> latest.source_digest_sha256
    THEN
        RETURN false;
    END IF;

    SELECT * INTO intent
      FROM internal_rpc_authority.authority_rotation_intents
     WHERE intent_id = p_publication_intent_id
     FOR UPDATE;
    IF FOUND THEN
        IF intent.source_revision <> p_source_revision
           OR intent.protocol_version <> 2
           OR intent.status <> 'DELIVERING'
           OR intent.predecessor_revision <> p_predecessor_revision
           OR intent.predecessor_digest_sha256 <> p_predecessor_digest_sha256
           OR intent.expected_readback_count <> p_expected_readback_count
        THEN
            RETURN false;
        END IF;
        UPDATE internal_rpc_authority.authority_rotation_intents
           SET source_digest_sha256 = p_source_digest_sha256,
               updated_at = pg_catalog.clock_timestamp()
         WHERE intent_id = p_publication_intent_id
           AND source_digest_sha256 <> p_source_digest_sha256;
    ELSE
        -- Во время rolling update старый writer не может обойти уже
        -- зафиксированное version2 намерение новой replica.
        IF EXISTS (
            SELECT 1
            FROM internal_rpc_authority.authority_rotation_intents
            WHERE protocol_version = 2
              AND source_revision = p_source_revision
              AND status IN ('PREPARED', 'DELIVERING', 'DELIVERED')
        ) THEN
            RETURN false;
        END IF;
        INSERT INTO internal_rpc_authority.authority_rotation_intents (
            intent_id, source_revision, source_digest_sha256, status,
            protocol_version, registry_source_digest_sha256,
            created_at, updated_at
        ) VALUES (
            p_publication_intent_id, p_source_revision,
            p_source_digest_sha256, 'PREPARED', 1,
            p_source_digest_sha256,
            pg_catalog.clock_timestamp(), pg_catalog.clock_timestamp()
        );
    END IF;

    INSERT INTO internal_rpc_authority.authority_snapshot_history (
        source_revision, source_digest_sha256, key_set_revision,
        policy_revision, signer_generation, predecessor_revision,
        predecessor_digest_sha256, canonical_payload, published_at,
        snapshot_compact_jws, publication_intent_id,
        publication_input_digest_sha256, expected_readback_count
    ) VALUES (
        p_source_revision, p_source_digest_sha256, p_key_set_revision,
        p_policy_revision, p_signer_generation, p_predecessor_revision,
        p_predecessor_digest_sha256,
        pg_catalog.jsonb_build_object(
            'source_revision', p_source_revision,
            'source_digest_sha256', p_source_digest_sha256
        ),
        p_published_at, p_snapshot_compact_jws, p_publication_intent_id,
        p_publication_input_digest_sha256, p_expected_readback_count
    );
    RETURN true;
END
$$;
-- +goose StatementEnd

-- Promotion version=2 разрешён только после полной доставки и точного
-- readback каждого обязательного consumer. Старый version=1 сохраняет прежний
-- expand-контракт до отдельной активации нового writer.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION internal_rpc_authority.publisher_promote_snapshot(
    p_publication_intent_id uuid,
    p_source_revision bigint,
    p_source_digest_sha256 text,
    p_expected_readback_count integer,
    p_expected_workload_ids text[],
    p_expected_roles text[],
    p_expected_workload_generations bigint[]
) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
AS $$
DECLARE
    matched integer;
    expected_total integer;
    expected_unique integer;
    invalid_expected integer;
    publication internal_rpc_authority.authority_snapshot_history%ROWTYPE;
    intent internal_rpc_authority.authority_rotation_intents%ROWTYPE;
    promoted_time timestamp with time zone;
BEGIN
    IF NOT pg_catalog.pg_has_role(session_user, 'internal_rpc_authority_publisher', 'MEMBER')
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
       OR p_source_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_expected_readback_count NOT BETWEEN 1 AND 384
       OR pg_catalog.cardinality(p_expected_workload_ids) IS DISTINCT FROM p_expected_readback_count
       OR pg_catalog.cardinality(p_expected_roles) IS DISTINCT FROM p_expected_readback_count
       OR pg_catalog.cardinality(p_expected_workload_generations) IS DISTINCT FROM p_expected_readback_count
    THEN
        RETURN false;
    END IF;
    SELECT pg_catalog.count(*)::integer,
           pg_catalog.count(DISTINCT ROW(expected.workload_id, expected.role, expected.workload_generation))::integer,
           pg_catalog.count(*) FILTER (
               WHERE expected.workload_id IS NULL
                  OR expected.workload_id !~ '^[a-z0-9](?:[a-z0-9.-]{1,94}[a-z0-9])$'
                  OR expected.role NOT IN ('AUTHORIZATION_ISSUER', 'AUTHORIZATION_VERIFIER', 'AUTHORITY_PROOF_RESOLVER')
                  OR expected.workload_generation NOT BETWEEN 1 AND 9007199254740991
           )::integer
      INTO expected_total, expected_unique, invalid_expected
      FROM ROWS FROM (
          pg_catalog.unnest(p_expected_workload_ids),
          pg_catalog.unnest(p_expected_roles),
          pg_catalog.unnest(p_expected_workload_generations)
      ) AS expected(workload_id, role, workload_generation);
    IF expected_total <> p_expected_readback_count
       OR expected_unique <> p_expected_readback_count
       OR invalid_expected <> 0
    THEN
        RETURN false;
    END IF;
    SELECT * INTO publication
      FROM internal_rpc_authority.authority_snapshot_history
     WHERE publication_intent_id = p_publication_intent_id
       AND source_revision = p_source_revision
       AND source_digest_sha256 = p_source_digest_sha256
       AND expected_readback_count = p_expected_readback_count
     FOR UPDATE;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    SELECT * INTO intent
      FROM internal_rpc_authority.authority_rotation_intents
     WHERE intent_id = p_publication_intent_id
     FOR UPDATE;
    IF NOT FOUND
       OR intent.protocol_version = 2 AND intent.status NOT IN ('DELIVERED', 'PROMOTED')
       OR intent.protocol_version = 1 AND intent.status NOT IN ('PREPARED', 'DELIVERED', 'PROMOTED')
    THEN
        RETURN false;
    END IF;
    SELECT pg_catalog.count(*)::integer INTO matched
      FROM ROWS FROM (
          pg_catalog.unnest(p_expected_workload_ids),
          pg_catalog.unnest(p_expected_roles),
          pg_catalog.unnest(p_expected_workload_generations)
      ) AS expected(workload_id, role, workload_generation)
      JOIN internal_rpc_authority.authority_snapshot_readbacks AS readback
        ON readback.workload_id = expected.workload_id
       AND readback.role = expected.role
       AND readback.workload_generation = expected.workload_generation
       AND readback.source_revision = p_source_revision
       AND readback.digest_sha256 = p_source_digest_sha256;
    IF matched <> p_expected_readback_count THEN
        RETURN false;
    END IF;
    promoted_time := pg_catalog.clock_timestamp();
    UPDATE internal_rpc_authority.authority_rotation_intents
       SET status = 'PROMOTED',
           delivery_started_at = COALESCE(delivery_started_at, created_at),
           delivered_at = COALESCE(delivered_at, updated_at),
           promoted_at = COALESCE(promoted_at, promoted_time),
           overlap_until = COALESCE(overlap_until, promoted_time + interval '40 seconds'),
           updated_at = promoted_time
     WHERE intent_id = p_publication_intent_id
       AND status IN ('PREPARED', 'DELIVERED', 'PROMOTED');
    RETURN FOUND;
END
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION internal_rpc_authority.publisher_prepare_rotation(uuid, bigint, text, bigint, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION internal_rpc_authority.publisher_begin_rotation_delivery(uuid, bigint, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION internal_rpc_authority.publisher_mark_rotation_delivered(uuid, bigint, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION internal_rpc_authority.publisher_abort_rotation(uuid, bigint, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION internal_rpc_authority.authority_rotation_status() FROM PUBLIC;

GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_prepare_rotation(uuid, bigint, text, bigint, text, integer) TO internal_rpc_authority_publisher;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_begin_rotation_delivery(uuid, bigint, text) TO internal_rpc_authority_publisher;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_mark_rotation_delivered(uuid, bigint, text) TO internal_rpc_authority_publisher;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_abort_rotation(uuid, bigint, text) TO internal_rpc_authority_publisher;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.publisher_abort_rotation(uuid, bigint, text) TO internal_rpc_authority_migrator;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.authority_rotation_status() TO internal_rpc_authority_migrator;

RESET ROLE;

-- +goose Down
SELECT 1;
