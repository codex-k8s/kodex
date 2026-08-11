-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Owner wave 2 замыкает owner-gate graph, пропорциональный provider pool,
-- tenant-scoped outbox repair и исполнимый bootstrap PostgreSQL LOGIN.
RESET ROLE;

-- +goose StatementBegin
DO $role_safety$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_roles
        WHERE rolname = 'control_plane_role_controller'
          AND (rolsuper OR rolcreatedb OR rolreplication OR rolbypassrls)
    ) THEN
        RAISE EXCEPTION 'control-plane role controller has prohibited attributes'
            USING ERRCODE = '42501';
    END IF;
END
$role_safety$;
-- +goose StatementEnd

ALTER ROLE control_plane_role_controller
    NOLOGIN CREATEROLE INHERIT;
GRANT pg_signal_backend TO control_plane_role_controller;
SET ROLE control_plane_owner;
GRANT USAGE ON SCHEMA control_plane_extensions
    TO control_plane_role_controller;
GRANT EXECUTE ON FUNCTION
    control_plane_extensions.digest(bytea, text),
    control_plane_extensions.digest(text, text),
    control_plane_extensions.hmac(bytea, bytea, text),
    control_plane_extensions.hmac(text, text, text)
    TO control_plane_role_controller;

-- Функция принадлежит только привилегированному migration principal. Runtime
-- не получает CREATEROLE и не может выбрать имя вне точного поколения.
CREATE FUNCTION control_plane.bootstrap_runtime_principal(
    requested_name text,
    requested_generation bigint,
    requested_password text
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE
    high_watermark bigint;
    role_exists boolean;
    role_safe boolean;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_owner', 'member')
       OR requested_generation NOT BETWEEN 1 AND 9007199254740991
       OR requested_name <>
          ('control_plane_runtime_g' || requested_generation::text)
       OR octet_length(requested_password) NOT BETWEEN 24 AND 512 THEN
        RAISE EXCEPTION 'runtime principal bootstrap input is invalid'
            USING ERRCODE = '22023';
    END IF;
    SELECT generation_high_watermark
      INTO high_watermark
      FROM control_plane.runtime_principal_lifecycle
     WHERE singleton = true
     FOR UPDATE;
    IF requested_generation <= high_watermark
       AND NOT EXISTS (
            SELECT 1
              FROM control_plane.runtime_principals
             WHERE principal_name::text = requested_name
               AND generation = requested_generation
               AND status <> 'RETIRED'
       ) THEN
        RAISE EXCEPTION 'runtime principal resurrection is forbidden'
            USING ERRCODE = '55000';
    END IF;
    SELECT true,
           role.rolcanlogin
           AND NOT role.rolsuper
           AND NOT role.rolcreatedb
           AND NOT role.rolcreaterole
           AND NOT role.rolreplication
           AND NOT role.rolbypassrls
      INTO role_exists, role_safe
      FROM pg_catalog.pg_roles AS role
     WHERE role.rolname = requested_name;
    IF coalesce(role_exists, false) AND NOT role_safe THEN
        RAISE EXCEPTION 'existing runtime principal role is unsafe'
            USING ERRCODE = '42501';
    END IF;
    IF NOT coalesce(role_exists, false) THEN
        EXECUTE format(
            'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS',
            requested_name,
            requested_password
        );
    ELSE
        EXECUTE format(
            'ALTER ROLE %I LOGIN PASSWORD %L NOCREATEROLE NOINHERIT',
            requested_name,
            requested_password
        );
    END IF;
    EXECUTE format('GRANT control_plane_runtime TO %I', requested_name);
    EXECUTE format(
        'GRANT %I TO control_plane_role_controller WITH ADMIN OPTION',
        requested_name
    );
    IF NOT pg_has_role(requested_name, 'control_plane_runtime', 'member')
       OR NOT pg_has_role(
            'control_plane_role_controller', requested_name, 'member'
       ) THEN
        RAISE EXCEPTION 'runtime principal bootstrap readback failed'
            USING ERRCODE = '55000';
    END IF;
END
$function$;
REVOKE ALL ON FUNCTION control_plane.bootstrap_runtime_principal(
    text, bigint, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.bootstrap_runtime_principal(
    text, bigint, text
) TO control_plane_owner;
ALTER FUNCTION control_plane.bootstrap_runtime_principal(text, bigint, text)
    OWNER TO control_plane_role_controller;

SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

-- Старые process rows получают точные версии их immutable lineage до того,
-- как новый бинарь начнёт строгий decode typed spec.
UPDATE control_plane.resources AS process
SET spec = process.spec
    || jsonb_build_object(
        'rootSessionVersion', (
            SELECT root_session.version
              FROM control_plane.resources AS root_session
             WHERE root_session.id =
                   (process.spec ->> 'rootSessionId')::uuid
        ),
        'rootTurnVersion', (
            SELECT root_turn.version
              FROM control_plane.resources AS root_turn
             WHERE root_turn.id = (process.spec ->> 'rootTurnId')::uuid
        )
    )
    || CASE
        WHEN coalesce(process.spec ->> 'targetSessionId', '') = '' THEN '{}'::jsonb
        ELSE jsonb_build_object(
            'targetSessionVersion', (
                SELECT target_session.version
                  FROM control_plane.resources AS target_session
                 WHERE target_session.id =
                       (process.spec ->> 'targetSessionId')::uuid
            ),
            'targetTurnVersion', (
                SELECT target_turn.version
                  FROM control_plane.resources AS target_turn
                 WHERE target_turn.id =
                       (process.spec ->> 'targetTurnId')::uuid
            )
        )
       END
WHERE process.kind = 'PROCESS_RUN';

-- WAITING_OWNER не имеет scheduler lease: recovery CLAIMED его не видит.
ALTER TABLE control_plane.schedule_occurrences
    DROP CONSTRAINT schedule_occurrences_state_check,
    DROP CONSTRAINT schedule_occurrence_lease_consistency;
ALTER TABLE control_plane.schedule_occurrences
    ADD CONSTRAINT schedule_occurrences_state_check CHECK (state IN (
        'QUEUED', 'CLAIMED', 'WAITING_OWNER', 'SUCCEEDED', 'FAILED',
        'CANCELLED', 'SKIPPED', 'DEAD_LETTER'
    )),
    ADD CONSTRAINT schedule_occurrence_lease_consistency CHECK (
        (
            state = 'CLAIMED'
            AND claimant_workload_id IS NOT NULL
            AND authority_generation IS NOT NULL
            AND token_hash ~ '^[a-f0-9]{64}$'
            AND lease_expires_at IS NOT NULL
        )
        OR (
            state <> 'CLAIMED'
            AND claimant_workload_id IS NULL
            AND authority_generation IS NULL
            AND token_hash IS NULL
            AND lease_expires_at IS NULL
        )
    );
ALTER TABLE control_plane.scheduled_runs
    DROP CONSTRAINT scheduled_runs_state_check;
-- +goose StatementBegin
DO $scheduled_run_finished_constraint$
DECLARE
    constraint_name name;
BEGIN
    FOR constraint_name IN
        SELECT constraint_data.conname
          FROM pg_catalog.pg_constraint AS constraint_data
         WHERE constraint_data.conrelid = 'control_plane.scheduled_runs'::regclass
           AND constraint_data.contype = 'c'
           AND pg_catalog.pg_get_constraintdef(constraint_data.oid) LIKE '%finished_at%'
           AND pg_catalog.pg_get_constraintdef(constraint_data.oid) LIKE '%CLAIMED%'
    LOOP
        EXECUTE format(
            'ALTER TABLE control_plane.scheduled_runs DROP CONSTRAINT %I',
            constraint_name
        );
    END LOOP;
END
$scheduled_run_finished_constraint$;
-- +goose StatementEnd
ALTER TABLE control_plane.scheduled_runs
    ADD CONSTRAINT scheduled_runs_state_check CHECK (state IN (
        'CLAIMED', 'WAITING_OWNER', 'SUCCEEDED', 'FAILED', 'CANCELLED'
    )),
    ADD CONSTRAINT scheduled_runs_finished_consistency CHECK (
        (state IN ('CLAIMED', 'WAITING_OWNER')) = (finished_at IS NULL)
    );

-- Позиция цикла хранится авторитетно и сбрасывается только новой policy или
-- новым exact eligibility snapshot. За полный цикл весов получается точная
-- пропорция, а concurrent selections сериализуются одной строкой.
CREATE TABLE control_plane.provider_pool_cursors (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    role_id uuid NOT NULL REFERENCES control_plane.resources (id),
    policy_revision bigint NOT NULL CHECK (policy_revision > 0),
    snapshot_sha256 text NOT NULL CHECK (snapshot_sha256 ~ '^[a-f0-9]{64}$'),
    total_weight bigint NOT NULL CHECK (total_weight BETWEEN 1 AND 80000),
    next_slot bigint NOT NULL CHECK (next_slot BETWEEN 0 AND 79999),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, role_id)
);
ALTER TABLE control_plane.provider_pool_cursors ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.provider_pool_cursors FORCE ROW LEVEL SECURITY;
CREATE POLICY provider_pool_cursors_runtime_scope
    ON control_plane.provider_pool_cursors
    FOR ALL TO control_plane_runtime, control_plane_owner
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );
CREATE FUNCTION control_plane.next_provider_pool_slot(
    requested_role_id uuid,
    requested_policy_revision bigint,
    requested_snapshot_sha256 text,
    requested_total_weight bigint
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
SET row_security = on
AS $function$
DECLARE
    scope record;
    cursor_row control_plane.provider_pool_cursors%ROWTYPE;
    selected_slot bigint;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_runtime', 'member')
       OR requested_policy_revision < 1
       OR requested_snapshot_sha256 !~ '^[a-f0-9]{64}$'
       OR requested_total_weight NOT BETWEEN 1 AND 80000 THEN
        RAISE EXCEPTION 'provider pool cursor input is invalid'
            USING ERRCODE = '22023';
    END IF;
    SELECT * INTO scope FROM control_plane.runtime_scope();
    IF NOT EXISTS (
        SELECT 1 FROM control_plane.resources AS role
         WHERE role.id = requested_role_id
           AND role.organization_id = scope.organization_id
           AND role.project_id = scope.project_id
           AND role.kind = 'ROLE'
           AND role.state = 'ACTIVE'
    ) THEN
        RAISE EXCEPTION 'provider pool role is unavailable'
            USING ERRCODE = 'P0002';
    END IF;
    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            scope.organization_id::text || ':' ||
            scope.project_id::text || ':' || requested_role_id::text,
            0
        )
    );
    SELECT *
      INTO cursor_row
      FROM control_plane.provider_pool_cursors
     WHERE organization_id = scope.organization_id
       AND project_id = scope.project_id
       AND role_id = requested_role_id
     FOR UPDATE;
    IF NOT FOUND THEN
        selected_slot := 0;
        INSERT INTO control_plane.provider_pool_cursors (
            organization_id, project_id, role_id, policy_revision,
            snapshot_sha256, total_weight, next_slot, updated_at
        ) VALUES (
            scope.organization_id, scope.project_id, requested_role_id,
            requested_policy_revision, requested_snapshot_sha256,
            requested_total_weight, 1 % requested_total_weight,
            clock_timestamp()
        );
    ELSIF cursor_row.policy_revision <> requested_policy_revision
       OR cursor_row.snapshot_sha256 <> requested_snapshot_sha256
       OR cursor_row.total_weight <> requested_total_weight THEN
        selected_slot := 0;
        UPDATE control_plane.provider_pool_cursors
           SET policy_revision = requested_policy_revision,
               snapshot_sha256 = requested_snapshot_sha256,
               total_weight = requested_total_weight,
               next_slot = 1 % requested_total_weight,
               updated_at = clock_timestamp()
         WHERE organization_id = scope.organization_id
           AND project_id = scope.project_id
           AND role_id = requested_role_id;
    ELSE
        selected_slot := cursor_row.next_slot;
        UPDATE control_plane.provider_pool_cursors
           SET next_slot = (cursor_row.next_slot + 1) % requested_total_weight,
               updated_at = clock_timestamp()
         WHERE organization_id = scope.organization_id
           AND project_id = scope.project_id
           AND role_id = requested_role_id;
    END IF;
    RETURN selected_slot;
END
$function$;
REVOKE ALL ON FUNCTION control_plane.next_provider_pool_slot(
    uuid, bigint, text, bigint
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.next_provider_pool_slot(
    uuid, bigint, text, bigint
) TO control_plane_runtime;

-- Все generic single/list/search read paths отбрасывают ACTIVE WorkClaim,
-- если авторитетный execution graph уже закрыт или сменил attempt/grant.
CREATE FUNCTION control_plane.work_claim_graph_is_active(
    claim control_plane.resources
) RETURNS boolean
LANGUAGE sql
STABLE
SECURITY INVOKER
SET search_path = pg_catalog, control_plane
AS $function$
    SELECT claim.kind = 'WORK_CLAIM'
       AND claim.state = 'ACTIVE'
       AND EXISTS (
            SELECT 1
              FROM control_plane.resources AS session
             WHERE session.id = (claim.spec ->> 'sessionId')::uuid
               AND session.organization_id = claim.organization_id
               AND session.project_id = claim.project_id
               AND session.kind = 'SESSION'
               AND session.state = 'ACTIVE'
               AND session.owner_actor_id = claim.owner_actor_id
       )
       AND EXISTS (
            SELECT 1
              FROM control_plane.resources AS turn
              JOIN control_plane.turn_attempts AS attempt
                ON attempt.turn_id = turn.id
               AND attempt.attempt = (claim.spec ->> 'attempt')::integer
               AND attempt.state IN ('QUEUED', 'CLAIMED')
               AND attempt.finished_at IS NULL
               AND attempt.authority_generation =
                   (claim.spec ->> 'authorityGeneration')::bigint
             WHERE turn.id = (claim.spec ->> 'turnId')::uuid
               AND turn.organization_id = claim.organization_id
               AND turn.project_id = claim.project_id
               AND turn.kind = 'TURN'
               AND turn.state NOT IN (
                    'SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DELETED'
               )
               AND turn.owner_actor_id = claim.owner_actor_id
               AND turn.spec ->> 'sessionId' = claim.spec ->> 'sessionId'
               AND turn.spec ->> 'processRunId' = claim.spec ->> 'processRunId'
               AND turn.spec ->> 'attempt' = claim.spec ->> 'attempt'
       )
       AND EXISTS (
            SELECT 1
              FROM control_plane.resources AS process
             WHERE process.id = (claim.spec ->> 'processRunId')::uuid
               AND process.organization_id = claim.organization_id
               AND process.project_id = claim.project_id
               AND process.kind = 'PROCESS_RUN'
               AND process.state NOT IN (
                    'SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DELETED'
               )
               AND process.owner_actor_id = claim.owner_actor_id
               AND (
                    (
                        coalesce(process.spec ->> 'parentProcessRunId', '') = ''
                        AND process.spec ->> 'rootSessionId' =
                            claim.spec ->> 'sessionId'
                        AND process.spec ->> 'rootTurnId' =
                            claim.spec ->> 'turnId'
                        AND process.spec ->> 'rootAttempt' =
                            claim.spec ->> 'attempt'
                    )
                    OR (
                        coalesce(process.spec ->> 'parentProcessRunId', '') <> ''
                        AND process.spec ->> 'targetSessionId' =
                            claim.spec ->> 'sessionId'
                        AND process.spec ->> 'targetTurnId' =
                            claim.spec ->> 'turnId'
                        AND process.spec ->> 'targetAttempt' =
                            claim.spec ->> 'attempt'
                    )
               )
       )
$function$;
REVOKE ALL ON FUNCTION control_plane.work_claim_graph_is_active(
    control_plane.resources
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.work_claim_graph_is_active(
    control_plane.resources
) TO control_plane_runtime, control_plane_owner;

-- Repair receipt теперь имеет exact tenant/ordering/sequence/operation scope.
ALTER TABLE control_plane.outbox_repairs
    ADD COLUMN organization_id uuid,
    ADD COLUMN project_id uuid,
    ADD COLUMN ordering_key text,
    ADD COLUMN event_sequence bigint,
    ADD COLUMN operation text DEFAULT 'REQUEUE',
    ADD COLUMN result_event_name text,
    ADD COLUMN result_aggregate_id uuid,
    ADD COLUMN result_attempts integer,
    ADD COLUMN result_repair_count integer,
    ADD COLUMN result_last_error_class text,
    ADD COLUMN result_occurred_at timestamptz,
    ADD COLUMN result_updated_at timestamptz;
UPDATE control_plane.outbox_repairs AS repair
SET organization_id = event.organization_id,
    project_id = event.project_id,
    ordering_key = event.ordering_key,
    event_sequence = event.event_sequence,
    operation = 'REQUEUE',
    result_event_name = event.event_name,
    result_aggregate_id = event.aggregate_id,
    result_attempts = event.attempts,
    result_repair_count = event.repair_count,
    result_last_error_class = coalesce(event.last_error_class, ''),
    result_occurred_at = event.occurred_at,
    result_updated_at = event.updated_at
FROM control_plane.outbox_events AS event
WHERE event.event_id = repair.event_id;
ALTER TABLE control_plane.outbox_repairs
    ALTER COLUMN organization_id SET NOT NULL,
    ALTER COLUMN project_id SET NOT NULL,
    ALTER COLUMN ordering_key SET NOT NULL,
    ALTER COLUMN event_sequence SET NOT NULL,
    ALTER COLUMN operation SET NOT NULL,
    ALTER COLUMN result_event_name SET NOT NULL,
    ALTER COLUMN result_aggregate_id SET NOT NULL,
    ALTER COLUMN result_attempts SET NOT NULL,
    ALTER COLUMN result_repair_count SET NOT NULL,
    ALTER COLUMN result_last_error_class SET NOT NULL,
    ALTER COLUMN result_occurred_at SET NOT NULL,
    ALTER COLUMN result_updated_at SET NOT NULL,
    DROP CONSTRAINT outbox_repairs_pkey,
    ADD CONSTRAINT outbox_repairs_pkey PRIMARY KEY (
        organization_id, project_id, operation, idempotency_key_hash
    ),
    ADD CONSTRAINT outbox_repairs_operation_closed CHECK (
        operation = 'REQUEUE'
    ),
    ADD CONSTRAINT outbox_repairs_sequence_positive CHECK (
        event_sequence > 0
    ),
    ADD CONSTRAINT outbox_repairs_result_closed CHECK (
        result_attempts = 0
        AND result_repair_count BETWEEN 1 AND 5
        AND octet_length(result_event_name) BETWEEN 1 AND 160
        AND octet_length(result_last_error_class) <= 160
    );
DROP POLICY outbox_repairs_runtime_scope ON control_plane.outbox_repairs;
DROP POLICY outbox_repairs_owner_bounded_insert ON control_plane.outbox_repairs;
CREATE POLICY outbox_repairs_runtime_scope ON control_plane.outbox_repairs
    FOR SELECT TO control_plane_runtime, control_plane_owner
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );
CREATE POLICY outbox_repairs_owner_bounded_insert ON control_plane.outbox_repairs
    FOR INSERT TO control_plane_owner
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );

CREATE OR REPLACE FUNCTION control_plane.repair_terminal_outbox_event(
    requested_event_id uuid,
    requested_sequence bigint,
    requested_attempts integer,
    requested_idempotency_key_hash text,
    requested_request_hash text,
    requested_reason_code text,
    requested_evidence_sha256 text,
    requested_actor_id uuid,
    requested_correlation_id uuid,
    requested_policy_revision bigint,
    requested_repaired_at timestamptz
)
RETURNS TABLE (
    event_id text,
    ordering_key text,
    event_sequence bigint,
    event_name text,
    aggregate_id text,
    attempts integer,
    repair_count integer,
    last_error_class text,
    occurred_at timestamptz,
    updated_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
SET row_security = on
AS $function$
DECLARE
    scope record;
    event_row control_plane.outbox_events%ROWTYPE;
    receipt_row control_plane.outbox_repairs%ROWTYPE;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime outbox repair role is required'
            USING ERRCODE = '42501';
    END IF;
    SELECT * INTO scope FROM control_plane.runtime_scope();
    IF requested_actor_id <> scope.actor_id
       OR requested_sequence < 1
       OR requested_attempts NOT BETWEEN 1 AND 100
       OR requested_idempotency_key_hash !~ '^[a-f0-9]{64}$'
       OR requested_request_hash !~ '^[a-f0-9]{64}$'
       OR requested_reason_code !~ '^[a-z][a-z0-9._-]{0,95}$'
       OR requested_evidence_sha256 !~ '^[a-f0-9]{64}$'
       OR requested_policy_revision < 1
       OR requested_repaired_at < clock_timestamp() - interval '5 minutes'
       OR requested_repaired_at > clock_timestamp() + interval '1 minute' THEN
        RAISE EXCEPTION 'outbox repair request is invalid'
            USING ERRCODE = '22023';
    END IF;
    SELECT *
      INTO receipt_row
      FROM control_plane.outbox_repairs AS receipt
     WHERE receipt.organization_id = scope.organization_id
       AND receipt.project_id = scope.project_id
       AND receipt.operation = 'REQUEUE'
       AND receipt.idempotency_key_hash = requested_idempotency_key_hash;
    IF FOUND THEN
        IF receipt_row.request_hash <> requested_request_hash
           OR receipt_row.event_id <> requested_event_id
           OR receipt_row.event_sequence <> requested_sequence THEN
            RAISE EXCEPTION 'outbox repair idempotency conflict'
                USING ERRCODE = '23505';
        END IF;
        RETURN QUERY
        SELECT
            receipt_row.event_id::text,
            receipt_row.ordering_key,
            receipt_row.event_sequence,
            receipt_row.result_event_name,
            receipt_row.result_aggregate_id::text,
            receipt_row.result_attempts,
            receipt_row.result_repair_count,
            receipt_row.result_last_error_class,
            receipt_row.result_occurred_at,
            receipt_row.result_updated_at;
        RETURN;
    END IF;
    SELECT *
      INTO event_row
      FROM control_plane.outbox_events AS event
     WHERE event.event_id = requested_event_id
       AND event.organization_id = scope.organization_id
       AND event.project_id = scope.project_id
       AND event.terminal = true
       AND event.published_at IS NULL
       AND event.event_sequence = requested_sequence
       AND event.attempts = requested_attempts
       AND event.repair_count < 5
       AND NOT EXISTS (
            SELECT 1
              FROM control_plane.outbox_events AS predecessor
             WHERE predecessor.organization_id = event.organization_id
               AND predecessor.project_id = event.project_id
               AND predecessor.ordering_key = event.ordering_key
               AND predecessor.event_sequence < event.event_sequence
               AND predecessor.published_at IS NULL
       )
     FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'terminal outbox event is not repairable'
            USING ERRCODE = '55000';
    END IF;
    INSERT INTO control_plane.outbox_repairs (
        organization_id, project_id, ordering_key, event_sequence, operation,
        event_id, idempotency_key_hash, request_hash, reason_code,
        evidence_sha256, actor_id, correlation_id, policy_revision, repaired_at,
        result_event_name, result_aggregate_id, result_attempts,
        result_repair_count, result_last_error_class, result_occurred_at,
        result_updated_at
    ) VALUES (
        scope.organization_id, scope.project_id, event_row.ordering_key,
        event_row.event_sequence, 'REQUEUE', event_row.event_id,
        requested_idempotency_key_hash, requested_request_hash,
        requested_reason_code, requested_evidence_sha256, requested_actor_id,
        requested_correlation_id, requested_policy_revision,
        requested_repaired_at, event_row.event_name, event_row.aggregate_id,
        0, event_row.repair_count + 1, '', event_row.occurred_at,
        requested_repaired_at
    );
    UPDATE control_plane.outbox_events AS event
       SET terminal = false,
           attempts = 0,
           repair_count = event.repair_count + 1,
           available_at = requested_repaired_at,
           last_error_class = NULL,
           lease_owner = NULL,
           lease_token = NULL,
           lease_until = NULL,
           updated_at = requested_repaired_at
     WHERE event.event_id = event_row.event_id;
    RETURN QUERY
    SELECT
        event.event_id::text,
        event.ordering_key,
        event.event_sequence,
        event.event_name,
        event.aggregate_id::text,
        event.attempts,
        event.repair_count,
        coalesce(event.last_error_class, ''),
        event.occurred_at,
        event.updated_at
    FROM control_plane.outbox_events AS event
    WHERE event.event_id = event_row.event_id;
END
$function$;

UPDATE control_plane.schema_state
SET version = 20260731000500, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260731000500 is forward-only: owner graph, provider cursor and tenant repair receipts cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
