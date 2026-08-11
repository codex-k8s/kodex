-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Owner wave 1 добавляет server-owned delegation/scheduled-run lineage,
-- ремонт terminal outbox и отдельную least-privilege роль ротации LOGIN.
RESET ROLE;

-- +goose StatementBegin
DO $roles$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_roles
        WHERE rolname = 'control_plane_role_controller'
    ) THEN
        CREATE ROLE control_plane_role_controller
            NOLOGIN NOSUPERUSER NOCREATEDB CREATEROLE NOINHERIT
            NOREPLICATION NOBYPASSRLS;
    END IF;
END
$roles$;
-- +goose StatementEnd
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
    NOLOGIN CREATEROLE NOINHERIT;
GRANT pg_signal_backend TO control_plane_role_controller;
GRANT control_plane_runtime TO control_plane_role_controller WITH ADMIN OPTION;
SET ROLE control_plane_owner;
GRANT USAGE ON SCHEMA control_plane TO control_plane_role_controller;
GRANT SELECT, INSERT, UPDATE ON
    control_plane.runtime_principals,
    control_plane.runtime_context_keys,
    control_plane.runtime_principal_lifecycle,
    control_plane.runtime_principal_readbacks
    TO control_plane_role_controller;

-- Каждая уже зарегистрированная LOGIN-роль делегируется controller с
-- ADMIN OPTION; новые поколения не пройдут readback/reconcile без такого же
-- code-first bootstrap grant.
RESET ROLE;
-- +goose StatementBegin
DO $managed_roles$
DECLARE
    managed_name name;
BEGIN
    FOR managed_name IN
        SELECT principal_name FROM control_plane.runtime_principals
    LOOP
        EXECUTE format(
            'GRANT %I TO control_plane_role_controller WITH ADMIN OPTION',
            managed_name
        );
    END LOOP;
END
$managed_roles$;
-- +goose StatementEnd

SET ROLE control_plane_owner;
ALTER FUNCTION control_plane.reconcile_runtime_principals(jsonb, text, bytea)
    OWNER TO control_plane_role_controller;
REVOKE ALL ON FUNCTION control_plane.reconcile_runtime_principals(jsonb, text, bytea)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.reconcile_runtime_principals(jsonb, text, bytea)
    TO control_plane_owner;

SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE FUNCTION control_plane.require_runtime_principal_controller()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    IF NEW.status <> 'RETIRED' AND NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_auth_members AS membership
        JOIN pg_catalog.pg_roles AS managed
          ON managed.oid = membership.roleid
        JOIN pg_catalog.pg_roles AS controller
          ON controller.oid = membership.member
        WHERE managed.rolname = NEW.principal_name
          AND controller.rolname = 'control_plane_role_controller'
          AND membership.admin_option = true
    ) THEN
        RAISE EXCEPTION 'runtime principal controller admin grant is required'
            USING ERRCODE = '42501';
    END IF;
    RETURN NEW;
END
$function$;
CREATE TRIGGER runtime_principal_controller_fence
BEFORE INSERT OR UPDATE OF principal_name, status
ON control_plane.runtime_principals
FOR EACH ROW EXECUTE FUNCTION control_plane.require_runtime_principal_controller();

CREATE TABLE control_plane.delegation_edges (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    parent_process_run_id uuid NOT NULL REFERENCES control_plane.resources (id),
    source_session_id uuid NOT NULL REFERENCES control_plane.resources (id),
    source_turn_id uuid NOT NULL REFERENCES control_plane.resources (id),
    source_attempt integer NOT NULL CHECK (source_attempt BETWEEN 1 AND 100),
    source_input_sha256 text NOT NULL CHECK (source_input_sha256 ~ '^[a-f0-9]{64}$'),
    target_session_id uuid NOT NULL REFERENCES control_plane.resources (id),
    target_role_id uuid NOT NULL REFERENCES control_plane.resources (id),
    target_turn_id uuid NOT NULL UNIQUE REFERENCES control_plane.resources (id),
    target_attempt integer NOT NULL CHECK (target_attempt BETWEEN 1 AND 100),
    target_input_sha256 text NOT NULL CHECK (target_input_sha256 ~ '^[a-f0-9]{64}$'),
    root_initiator_actor_id uuid NOT NULL,
    grant_generation bigint NOT NULL CHECK (grant_generation > 0),
    created_at timestamptz NOT NULL,
    UNIQUE (organization_id, project_id, id)
);
CREATE INDEX delegation_edges_source_idx
    ON control_plane.delegation_edges (
        organization_id, project_id, parent_process_run_id, source_turn_id
    );
ALTER TABLE control_plane.delegation_edges ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.delegation_edges FORCE ROW LEVEL SECURITY;
CREATE POLICY delegation_edges_runtime_scope ON control_plane.delegation_edges
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );

ALTER TABLE control_plane.schedule_occurrences
    ADD COLUMN execution_session_id uuid,
    ADD COLUMN execution_session_version bigint,
    ADD COLUMN execution_turn_id uuid,
    ADD COLUMN execution_turn_version bigint,
    ADD COLUMN execution_process_run_id uuid,
    ADD COLUMN execution_process_version bigint,
    ADD COLUMN execution_runtime_revision_id uuid,
    ADD COLUMN execution_runtime_revision_version bigint,
    ADD CONSTRAINT schedule_occurrence_execution_complete CHECK (
        (execution_session_id IS NULL
         AND execution_session_version IS NULL
         AND execution_turn_id IS NULL
         AND execution_turn_version IS NULL
         AND execution_process_run_id IS NULL
         AND execution_process_version IS NULL
         AND execution_runtime_revision_id IS NULL
         AND execution_runtime_revision_version IS NULL)
        OR
        (execution_session_id IS NOT NULL
         AND execution_session_version > 0
         AND execution_turn_id IS NOT NULL
         AND execution_turn_version > 0
         AND execution_runtime_revision_id IS NOT NULL
         AND execution_runtime_revision_version > 0
         AND ((execution_process_run_id IS NULL AND execution_process_version IS NULL)
              OR (execution_process_run_id IS NOT NULL AND execution_process_version > 0)))
    );

CREATE TABLE control_plane.scheduled_runs (
    occurrence_id uuid NOT NULL REFERENCES control_plane.schedule_occurrences (id),
    attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 100),
    session_id uuid NOT NULL REFERENCES control_plane.resources (id),
    session_version bigint NOT NULL CHECK (session_version > 0),
    turn_id uuid NOT NULL REFERENCES control_plane.resources (id),
    turn_version bigint NOT NULL CHECK (turn_version > 0),
    process_run_id uuid REFERENCES control_plane.resources (id),
    process_version bigint,
    runtime_revision_id uuid NOT NULL REFERENCES control_plane.resources (id),
    runtime_revision_version bigint NOT NULL CHECK (runtime_revision_version > 0),
    effective_input_sha256 text NOT NULL CHECK (effective_input_sha256 ~ '^[a-f0-9]{64}$'),
    state text NOT NULL CHECK (state IN ('CLAIMED', 'SUCCEEDED', 'FAILED', 'CANCELLED')),
    outcome text,
    result_artifact_id uuid REFERENCES control_plane.resources (id),
    created_at timestamptz NOT NULL,
    finished_at timestamptz,
    PRIMARY KEY (occurrence_id, attempt),
    CHECK ((process_run_id IS NULL) = (process_version IS NULL)),
    CHECK ((state = 'CLAIMED') = (finished_at IS NULL))
);
ALTER TABLE control_plane.scheduled_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.scheduled_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY scheduled_runs_runtime_scope ON control_plane.scheduled_runs
    FOR ALL TO control_plane_runtime
    USING (EXISTS (
        SELECT 1 FROM control_plane.schedule_occurrences AS occurrence
        WHERE occurrence.id = scheduled_runs.occurrence_id
          AND occurrence.organization_id = (control_plane.runtime_scope()).organization_id
          AND occurrence.project_id = (control_plane.runtime_scope()).project_id
    ))
    WITH CHECK (EXISTS (
        SELECT 1 FROM control_plane.schedule_occurrences AS occurrence
        WHERE occurrence.id = scheduled_runs.occurrence_id
          AND occurrence.organization_id = (control_plane.runtime_scope()).organization_id
          AND occurrence.project_id = (control_plane.runtime_scope()).project_id
    ));

ALTER TABLE control_plane.outbox_events
    ADD COLUMN repair_count integer NOT NULL DEFAULT 0
        CHECK (repair_count BETWEEN 0 AND 5);
CREATE TABLE control_plane.outbox_repairs (
    event_id uuid NOT NULL REFERENCES control_plane.outbox_events (event_id),
    idempotency_key_hash text PRIMARY KEY CHECK (idempotency_key_hash ~ '^[a-f0-9]{64}$'),
    request_hash text NOT NULL CHECK (request_hash ~ '^[a-f0-9]{64}$'),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9_-]{0,95}$'),
    evidence_sha256 text NOT NULL CHECK (evidence_sha256 ~ '^[a-f0-9]{64}$'),
    actor_id uuid NOT NULL,
    correlation_id uuid NOT NULL,
    policy_revision bigint NOT NULL CHECK (policy_revision > 0),
    repaired_at timestamptz NOT NULL
);
ALTER TABLE control_plane.outbox_repairs ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.outbox_repairs FORCE ROW LEVEL SECURITY;
CREATE POLICY outbox_repairs_runtime_scope ON control_plane.outbox_repairs
    FOR ALL TO control_plane_runtime
    USING (EXISTS (
        SELECT 1 FROM control_plane.outbox_events AS event
        WHERE event.event_id = outbox_repairs.event_id
          AND event.organization_id = (control_plane.runtime_scope()).organization_id
          AND event.project_id = (control_plane.runtime_scope()).project_id
    ))
    WITH CHECK (EXISTS (
        SELECT 1 FROM control_plane.outbox_events AS event
        WHERE event.event_id = outbox_repairs.event_id
          AND event.organization_id = (control_plane.runtime_scope()).organization_id
          AND event.project_id = (control_plane.runtime_scope()).project_id
    ));

CREATE POLICY outbox_runtime_metadata_read ON control_plane.outbox_events
    FOR SELECT TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );
CREATE POLICY outbox_owner_bounded_repair ON control_plane.outbox_events
    FOR ALL TO control_plane_owner
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );
CREATE POLICY outbox_repairs_owner_bounded_insert ON control_plane.outbox_repairs
    FOR INSERT TO control_plane_owner
    WITH CHECK (EXISTS (
        SELECT 1 FROM control_plane.outbox_events AS event
        WHERE event.event_id = outbox_repairs.event_id
          AND event.organization_id = (control_plane.runtime_scope()).organization_id
          AND event.project_id = (control_plane.runtime_scope()).project_id
    ));

CREATE FUNCTION control_plane.repair_terminal_outbox_event(
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
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime outbox repair role is required'
            USING ERRCODE = '42501';
    END IF;
    SELECT * INTO scope FROM control_plane.runtime_scope();
    RETURN QUERY
    WITH locked AS (
        SELECT event.event_id
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
              WHERE predecessor.ordering_key = event.ordering_key
                AND predecessor.event_sequence < event.event_sequence
                AND predecessor.published_at IS NULL
          )
        FOR UPDATE
    ), receipt AS (
        INSERT INTO control_plane.outbox_repairs (
            event_id, idempotency_key_hash, request_hash, reason_code,
            evidence_sha256, actor_id, correlation_id, policy_revision,
            repaired_at
        )
        SELECT
            locked.event_id, requested_idempotency_key_hash,
            requested_request_hash, requested_reason_code,
            requested_evidence_sha256, requested_actor_id,
            requested_correlation_id, requested_policy_revision,
            requested_repaired_at
        FROM locked
        ON CONFLICT (idempotency_key_hash) DO NOTHING
        RETURNING outbox_repairs.event_id
    ), repaired AS (
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
        FROM receipt
        WHERE event.event_id = receipt.event_id
        RETURNING event.*
    )
    SELECT
        repaired.event_id::text, repaired.ordering_key,
        repaired.event_sequence, repaired.event_name,
        repaired.aggregate_id::text, repaired.attempts,
        repaired.repair_count, coalesce(repaired.last_error_class, ''),
        repaired.occurred_at, repaired.updated_at
    FROM repaired;
END
$function$;
REVOKE ALL ON FUNCTION control_plane.repair_terminal_outbox_event(
    uuid, bigint, integer, text, text, text, text, uuid, uuid, bigint,
    timestamptz
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.repair_terminal_outbox_event(
    uuid, bigint, integer, text, text, text, text, uuid, uuid, bigint,
    timestamptz
) TO control_plane_runtime;

GRANT SELECT, INSERT ON control_plane.delegation_edges TO control_plane_runtime;
GRANT SELECT, INSERT, UPDATE ON control_plane.scheduled_runs TO control_plane_runtime;
GRANT SELECT, INSERT ON control_plane.outbox_repairs TO control_plane_runtime;
GRANT SELECT (
    event_id, organization_id, project_id, ordering_key, event_sequence,
    event_name, aggregate_id, attempts, repair_count, last_error_class,
    occurred_at, updated_at, terminal, published_at
) ON control_plane.outbox_events TO control_plane_runtime;

UPDATE control_plane.schema_state
SET version = 20260731000400, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260731000400 is forward-only: delegation, scheduled-run and outbox repair evidence cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
