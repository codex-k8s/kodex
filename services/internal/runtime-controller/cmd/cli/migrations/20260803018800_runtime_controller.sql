-- +goose Up
RESET ROLE;
SET ROLE runtime_controller_owner;
CREATE SCHEMA runtime_controller AUTHORIZATION runtime_controller_owner;
REVOKE ALL ON SCHEMA runtime_controller FROM PUBLIC;
SET search_path = runtime_controller, pg_catalog;

-- Нормативный schema contract postgresinbox версии 1.
-- Совместимость readiness contract: PostgreSQL 17+.
-- Библиотека не выполняет этот файл: сервис переносит DDL в собственную
-- forward-only goose migration и создаёт marker последним в одной транзакции.

CREATE TABLE runtime_event_schema_versions (
    component text PRIMARY KEY,
    version integer NOT NULL,
    schema_digest bytea NOT NULL,
    installed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT runtime_event_schema_versions_component_check
        CHECK (component ~ '^[a-z][a-z0-9_]{0,62}$'),
    CONSTRAINT runtime_event_schema_versions_version_check
        CHECK (version > 0),
    CONSTRAINT runtime_event_schema_versions_digest_check
        CHECK (octet_length(schema_digest) = 32)
);

CREATE FUNCTION runtime_event_ordering_key(
    organization_id text,
    event_name text,
    aggregate_type text,
    aggregate_id text
)
RETURNS jsonb
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog
AS $function$
    SELECT CASE
        WHEN organization_id IS NULL THEN
            jsonb_build_array(event_name, aggregate_type, aggregate_id)
        ELSE
            jsonb_build_array(
                organization_id,
                event_name,
                aggregate_type,
                aggregate_id
            )
    END
$function$;

-- PostgreSQL выдаёт PUBLIC EXECUTE на новые функции по умолчанию.
REVOKE ALL ON FUNCTION runtime_event_ordering_key(text, text, text, text)
    FROM PUBLIC;

CREATE TABLE runtime_event_cursors (
    consumer_name text NOT NULL,
    consumer_scope text NOT NULL,
    ordering_key jsonb NOT NULL,
    last_sequence bigint NOT NULL DEFAULT 0,
    last_event_id uuid,
    last_event_digest bytea,
    next_fence bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT runtime_event_cursors_pkey
        PRIMARY KEY (consumer_name, consumer_scope, ordering_key),
    CONSTRAINT runtime_event_cursors_consumer_name_check
        CHECK (consumer_name ~ '^[a-z][a-z0-9._-]{0,127}$'),
    CONSTRAINT runtime_event_cursors_consumer_scope_check
        CHECK (consumer_scope ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT runtime_event_cursors_ordering_key_check
        CHECK (
            jsonb_typeof(ordering_key) = 'array'
            AND jsonb_array_length(ordering_key) IN (3, 4)
            AND octet_length(ordering_key::text) BETWEEN 7 AND 1024
        ),
    CONSTRAINT runtime_event_cursors_sequence_check
        CHECK (last_sequence >= 0),
    CONSTRAINT runtime_event_cursors_fence_check
        CHECK (next_fence > 0),
    CONSTRAINT runtime_event_cursors_high_watermark_check
        CHECK (
            (
                last_sequence = 0
                AND last_event_id IS NULL
                AND last_event_digest IS NULL
            )
            OR (
                last_sequence > 0
                AND last_event_id IS NOT NULL
                AND octet_length(last_event_digest) = 32
            )
        )
);

CREATE TABLE runtime_inbox_events (
    consumer_name text NOT NULL,
    consumer_scope text NOT NULL,
    event_id uuid NOT NULL,
    event_digest bytea NOT NULL,
    event_name text NOT NULL,
    event_version integer NOT NULL,
    schema_version integer NOT NULL,
    occurred_at timestamptz NOT NULL,
    organization_id text,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    aggregate_version bigint NOT NULL,
    event_sequence bigint NOT NULL,
    ordering_key jsonb GENERATED ALWAYS AS (
        runtime_event_ordering_key(
            organization_id,
            event_name,
            aggregate_type,
            aggregate_id
        )
    ) STORED NOT NULL,
    state text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL,
    repair_count integer NOT NULL DEFAULT 0,
    max_repairs integer NOT NULL,
    lease_owner text,
    lease_token uuid,
    lease_generation bigint NOT NULL DEFAULT 0,
    lease_fence bigint NOT NULL DEFAULT 0,
    lease_expires_at timestamptz,
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    last_error_code text,
    received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    processed_at timestamptz,
    cleanup_after timestamptz,
    terminal_at timestamptz,
    CONSTRAINT runtime_inbox_events_pkey
        PRIMARY KEY (consumer_name, consumer_scope, event_id),
    CONSTRAINT runtime_inbox_events_sequence_key
        UNIQUE (consumer_name, consumer_scope, ordering_key, event_sequence),
    CONSTRAINT runtime_inbox_events_consumer_name_check
        CHECK (consumer_name ~ '^[a-z][a-z0-9._-]{0,127}$'),
    CONSTRAINT runtime_inbox_events_consumer_scope_check
        CHECK (consumer_scope ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT runtime_inbox_events_digest_check
        CHECK (octet_length(event_digest) = 32),
    CONSTRAINT runtime_inbox_events_event_name_check
        CHECK (char_length(event_name) BETWEEN 1 AND 128),
    CONSTRAINT runtime_inbox_events_event_version_check
        CHECK (event_version > 0),
    CONSTRAINT runtime_inbox_events_schema_version_check
        CHECK (schema_version > 0),
    CONSTRAINT runtime_inbox_events_organization_check
        CHECK (
            organization_id IS NULL
            OR char_length(organization_id) BETWEEN 1 AND 128
        ),
    CONSTRAINT runtime_inbox_events_aggregate_type_check
        CHECK (char_length(aggregate_type) BETWEEN 1 AND 64),
    CONSTRAINT runtime_inbox_events_aggregate_id_check
        CHECK (char_length(aggregate_id) BETWEEN 1 AND 128),
    CONSTRAINT runtime_inbox_events_aggregate_version_check
        CHECK (aggregate_version > 0),
    CONSTRAINT runtime_inbox_events_event_sequence_check
        CHECK (event_sequence > 0),
    CONSTRAINT runtime_inbox_events_ordering_key_check
        CHECK (
            jsonb_typeof(ordering_key) = 'array'
            AND jsonb_array_length(ordering_key) IN (3, 4)
            AND octet_length(ordering_key::text) BETWEEN 7 AND 1024
        ),
    CONSTRAINT runtime_inbox_events_state_check
        CHECK (
            state IN (
                'RECEIVED',
                'PROCESSING',
                'RETRY',
                'COMPLETED',
                'STALE',
                'DEAD_LETTER'
            )
        ),
    CONSTRAINT runtime_inbox_events_attempt_budget_check
        CHECK (
            attempts >= 0
            AND max_attempts BETWEEN 1 AND 100
            AND attempts <= max_attempts
        ),
    CONSTRAINT runtime_inbox_events_repair_budget_check
        CHECK (
            repair_count >= 0
            AND max_repairs BETWEEN 1 AND 20
            AND repair_count <= max_repairs
        ),
    CONSTRAINT runtime_inbox_events_lease_generation_check
        CHECK (
            lease_generation >= 0
            AND ((lease_generation = 0) = (lease_fence = 0))
        ),
    CONSTRAINT runtime_inbox_events_lease_fence_check
        CHECK (lease_fence >= 0),
    CONSTRAINT runtime_inbox_events_error_code_check
        CHECK (
            last_error_code IS NULL
            OR last_error_code ~ '^[a-z][a-z0-9_]{0,62}$'
        ),
    CONSTRAINT runtime_inbox_events_lease_consistency_check
        CHECK (
            (
                state = 'PROCESSING'
                AND lease_owner IS NOT NULL
                AND char_length(lease_owner) BETWEEN 1 AND 128
                AND lease_token IS NOT NULL
                AND lease_generation > 0
                AND lease_fence > 0
                AND lease_expires_at IS NOT NULL
            )
            OR (
                state <> 'PROCESSING'
                AND lease_owner IS NULL
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
            )
        ),
    CONSTRAINT runtime_inbox_events_terminal_consistency_check
        CHECK (
            (
                state IN ('COMPLETED', 'STALE')
                AND processed_at IS NOT NULL
                AND cleanup_after IS NOT NULL
                AND terminal_at IS NULL
            )
            OR (
                state = 'DEAD_LETTER'
                AND processed_at IS NULL
                AND cleanup_after IS NULL
                AND terminal_at IS NOT NULL
            )
            OR (
                state IN ('RECEIVED', 'PROCESSING', 'RETRY')
                AND processed_at IS NULL
                AND cleanup_after IS NULL
                AND terminal_at IS NULL
            )
        )
);

CREATE INDEX runtime_inbox_events_claim_idx
    ON runtime_inbox_events (
        consumer_name,
        consumer_scope,
        available_at,
        occurred_at,
        event_id
    )
    WHERE state IN ('RECEIVED', 'RETRY');

CREATE INDEX runtime_inbox_events_lease_idx
    ON runtime_inbox_events (
        lease_expires_at,
        consumer_name,
        consumer_scope,
        event_id
    )
    WHERE state = 'PROCESSING';

CREATE INDEX runtime_inbox_events_ordering_idx
    ON runtime_inbox_events (
        consumer_name,
        consumer_scope,
        ordering_key,
        event_sequence
    )
    WHERE state IN ('RECEIVED', 'PROCESSING', 'RETRY', 'DEAD_LETTER');

CREATE INDEX runtime_inbox_events_retention_idx
    ON runtime_inbox_events (
        cleanup_after,
        consumer_name,
        consumer_scope,
        event_id
    )
    WHERE state IN ('COMPLETED', 'STALE');

CREATE INDEX runtime_inbox_events_dead_letter_idx
    ON runtime_inbox_events (
        consumer_name,
        consumer_scope,
        terminal_at,
        event_id
    )
    WHERE state = 'DEAD_LETTER';

CREATE TABLE runtime_inbox_repairs (
    consumer_name text NOT NULL,
    consumer_scope text NOT NULL,
    organization_scope text NOT NULL,
    project_scope text NOT NULL,
    operation text NOT NULL,
    key_hash bytea NOT NULL,
    request_digest bytea NOT NULL,
    receipt_id uuid NOT NULL,
    event_id uuid NOT NULL,
    event_digest bytea NOT NULL,
    expected_generation bigint NOT NULL,
    expected_fence bigint NOT NULL,
    action text NOT NULL,
    actor text NOT NULL,
    reason text NOT NULL,
    evidence_digest bytea NOT NULL,
    result_generation bigint NOT NULL,
    result_fence bigint NOT NULL,
    result_directive text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT runtime_inbox_repairs_pkey
        PRIMARY KEY (organization_scope, project_scope, operation, key_hash),
    CONSTRAINT runtime_inbox_repairs_receipt_id_key UNIQUE (receipt_id),
    CONSTRAINT runtime_inbox_repairs_consumer_name_check
        CHECK (consumer_name ~ '^[a-z][a-z0-9._-]{0,127}$'),
    CONSTRAINT runtime_inbox_repairs_consumer_scope_check
        CHECK (consumer_scope ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'),
    CONSTRAINT runtime_inbox_repairs_authorized_scope_check
        CHECK (
            organization_scope ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'
            AND project_scope ~ '^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$'
            AND operation ~ '^[a-z][a-z0-9._-]{0,127}$'
            AND octet_length(key_hash) = 32
        ),
    CONSTRAINT runtime_inbox_repairs_request_digest_check
        CHECK (octet_length(request_digest) = 32),
    CONSTRAINT runtime_inbox_repairs_event_digest_check
        CHECK (octet_length(event_digest) = 32),
    CONSTRAINT runtime_inbox_repairs_expected_fence_check
        CHECK (
            expected_generation >= 0
            AND expected_fence >= 0
            AND ((expected_generation = 0) = (expected_fence = 0))
        ),
    CONSTRAINT runtime_inbox_repairs_action_check
        CHECK (action IN ('REQUEUE', 'REJOIN', 'TERMINALIZE', 'WAIT')),
    CONSTRAINT runtime_inbox_repairs_actor_check
        CHECK (char_length(actor) BETWEEN 1 AND 256),
    CONSTRAINT runtime_inbox_repairs_reason_check
        CHECK (char_length(reason) BETWEEN 1 AND 1024),
    CONSTRAINT runtime_inbox_repairs_evidence_digest_check
        CHECK (octet_length(evidence_digest) = 32),
    CONSTRAINT runtime_inbox_repairs_result_fence_check
        CHECK (
            result_generation >= 0
            AND result_fence >= 0
            AND result_generation = expected_generation
            AND result_fence = expected_fence
        ),
    CONSTRAINT runtime_inbox_repairs_result_directive_check
        CHECK (
            result_directive IN (
                'replay_required',
                'wait_predecessor',
                'wait_lease',
                'wait_backoff',
                'repair_required',
                'ack_eligible'
            )
        )
);

CREATE INDEX runtime_inbox_repairs_event_idx
    ON runtime_inbox_repairs (
        consumer_name,
        consumer_scope,
        event_id,
        created_at,
        receipt_id
    );

-- До marker service migration обязана отдельно выполнить для своего exact
-- runtime principal: REVOKE ALL ON SCHEMA ... FROM PUBLIC; GRANT USAGE без
-- grant option; exact table grants из README; EXECUTE без grant option на
-- ordering/effect functions после REVOKE FROM PUBLIC. Runtime principal не
-- владеет schema/объектами, не состоит в ролях владельцев и не получает
-- column-level ACL. Идентичность principal намеренно не является частью
-- общего provider-neutral DDL.

-- Marker создаётся только после всех готовых объектов в той же migration.
CREATE TABLE runtime_configuration_projection (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    resource_kind text NOT NULL,
    resource_id uuid NOT NULL,
    resource_version bigint NOT NULL CHECK (resource_version > 0),
    resource_state text NOT NULL CHECK (length(resource_state) BETWEEN 2 AND 32),
    event_id uuid NOT NULL,
    event_sequence bigint NOT NULL CHECK (event_sequence > 0),
    event_digest bytea NOT NULL CHECK (octet_length(event_digest) = 32),
    authoritative_snapshot jsonb NOT NULL CHECK (jsonb_typeof(authoritative_snapshot) = 'object'),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, project_id, resource_kind, resource_id)
);

CREATE FUNCTION apply_projection(input jsonb)
RETURNS jsonb
LANGUAGE plpgsql
VOLATILE
PARALLEL UNSAFE
SECURITY INVOKER
AS $function$
DECLARE
    envelope jsonb := input -> 'envelope';
    snapshot jsonb := input -> 'authoritative_snapshot';
    changed integer;
BEGIN
    IF jsonb_typeof(input) <> 'object'
       OR jsonb_typeof(envelope) <> 'object'
       OR jsonb_typeof(snapshot) <> 'object'
       OR envelope ->> 'eventName' <> 'control_plane.runtime_configuration_changed'
       OR (envelope ->> 'eventVersion')::integer <> 1
       OR (envelope ->> 'schemaVersion')::integer <> 1
       OR input ->> 'event_sha256' !~ '^[0-9a-f]{64}$'
       OR envelope ->> 'aggregateId' <> envelope #>> '{data,resourceId}'
       OR (envelope ->> 'aggregateVersion')::bigint <> (envelope #>> '{data,resourceVersion}')::bigint
       OR envelope ->> 'aggregateType' <> envelope #>> '{data,resourceKind}'
       OR envelope #>> '{data,resourceKind}' NOT IN (
            'PROJECT', 'TEAM', 'CHAT', 'ROLE', 'PROMPT_PROFILE',
            'CREDENTIAL_BINDING', 'REPOSITORY_WORKSPACE', 'INTEGRATION',
            'RUNTIME_REVISION', 'SESSION', 'TURN'
       )
       OR envelope #>> '{data,resourceState}' NOT IN (
            'ACTIVE', 'PAUSED', 'ARCHIVED', 'DELETION_PENDING', 'DELETED',
            'QUEUED', 'CLAIMED', 'RUNNING', 'WAITING_OWNER',
            'WAITING_EXTERNAL', 'SUCCEEDED', 'FAILED', 'CANCELLED',
            'EXPIRED', 'BLOCKED'
       )
    THEN
        RAISE EXCEPTION 'runtime projection input is invalid' USING ERRCODE = '22023';
    END IF;

    INSERT INTO runtime_configuration_projection (
        organization_id, project_id, resource_kind, resource_id,
        resource_version, resource_state, event_id, event_sequence,
        event_digest, authoritative_snapshot
    ) VALUES (
        (envelope ->> 'organizationId')::uuid,
        (envelope #>> '{data,projectId}')::uuid,
        envelope #>> '{data,resourceKind}',
        (envelope #>> '{data,resourceId}')::uuid,
        (envelope #>> '{data,resourceVersion}')::bigint,
        envelope #>> '{data,resourceState}',
        (envelope ->> 'eventId')::uuid,
        (envelope ->> 'eventSequence')::bigint,
        decode(input ->> 'event_sha256', 'hex'),
        snapshot
    )
    ON CONFLICT (organization_id, project_id, resource_kind, resource_id)
    DO UPDATE SET
        resource_version = EXCLUDED.resource_version,
        resource_state = EXCLUDED.resource_state,
        event_id = EXCLUDED.event_id,
        event_sequence = EXCLUDED.event_sequence,
        event_digest = EXCLUDED.event_digest,
        authoritative_snapshot = EXCLUDED.authoritative_snapshot,
        updated_at = clock_timestamp()
    WHERE runtime_configuration_projection.resource_version < EXCLUDED.resource_version;

    GET DIAGNOSTICS changed = ROW_COUNT;
    IF changed <> 1 THEN
        RAISE EXCEPTION 'runtime projection version did not advance' USING ERRCODE = '40001';
    END IF;
    RETURN jsonb_build_object('applied', true);
END
$function$;

REVOKE ALL ON FUNCTION apply_projection(jsonb) FROM PUBLIC;
GRANT USAGE ON SCHEMA runtime_controller TO runtime_controller_runtime_g1;
GRANT SELECT ON runtime_event_schema_versions TO runtime_controller_runtime_g1;
GRANT SELECT, INSERT, UPDATE ON runtime_event_cursors TO runtime_controller_runtime_g1;
GRANT SELECT, INSERT, UPDATE, DELETE ON runtime_inbox_events TO runtime_controller_runtime_g1;
GRANT SELECT, INSERT ON runtime_inbox_repairs TO runtime_controller_runtime_g1;
GRANT SELECT, INSERT, UPDATE ON runtime_configuration_projection TO runtime_controller_runtime_g1;
GRANT EXECUTE ON FUNCTION runtime_event_ordering_key(text, text, text, text) TO runtime_controller_runtime_g1;
GRANT EXECUTE ON FUNCTION apply_projection(jsonb) TO runtime_controller_runtime_g1;

INSERT INTO runtime_event_schema_versions (component, version, schema_digest)
VALUES ('postgresinbox', 1, decode('4c44aeb7b45033cd140b9d49db24d67d0ff620687249879d3274427e1e29d5f2', 'hex'));
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION 'runtime-controller inbox migration is forward-only' USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
