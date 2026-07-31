-- +goose Up
DO $roles$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'control_plane_owner') THEN
        CREATE ROLE control_plane_owner
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'control_plane_runtime') THEN
        CREATE ROLE control_plane_runtime
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'control_plane_relay') THEN
        CREATE ROLE control_plane_relay
            NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
END
$roles$;

ALTER ROLE control_plane_owner
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE control_plane_runtime
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE control_plane_relay
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

CREATE SCHEMA control_plane AUTHORIZATION control_plane_owner;
REVOKE ALL ON SCHEMA control_plane FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA control_plane TO control_plane_runtime, control_plane_relay;

SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE SEQUENCE control_plane.authority_proof_revision_seq
    AS bigint
    MINVALUE 1
    MAXVALUE 9007199254740991
    NO CYCLE;

CREATE TABLE control_plane.resources (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    parent_id uuid,
    owner_actor_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN (
        'PROJECT', 'TEAM', 'CHAT', 'ROLE', 'PROMPT_PROFILE',
        'CREDENTIAL_BINDING', 'REPOSITORY_WORKSPACE', 'INTEGRATION',
        'RUNTIME_REVISION', 'SESSION', 'TURN', 'PROCESS_RUN', 'SCHEDULE',
        'OWNER_GATE', 'MEMORY_RECORD', 'WORK_CLAIM', 'ARTIFACT'
    )),
    name text NOT NULL CHECK (
        length(name) BETWEEN 1 AND 160
        AND name = btrim(name)
        AND name !~ '[[:cntrl:]]'
    ),
    state text NOT NULL CHECK (state IN (
        'ACTIVE', 'PAUSED', 'ARCHIVED', 'DELETION_PENDING', 'DELETED',
        'QUEUED', 'CLAIMED', 'RUNNING', 'WAITING_OWNER', 'WAITING_EXTERNAL',
        'SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'BLOCKED'
    )),
    version bigint NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991),
    spec jsonb NOT NULL CHECK (
        jsonb_typeof(spec) = 'object'
        AND octet_length(spec::text) <= 65536
    ),
    schedule_next_run_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (organization_id, project_id, id),
    CHECK ((kind = 'PROJECT' AND project_id = id) OR kind <> 'PROJECT'),
    CHECK (
        (kind = 'SCHEDULE' AND schedule_next_run_at IS NOT NULL)
        OR (kind <> 'SCHEDULE' AND schedule_next_run_at IS NULL)
    )
);

CREATE INDEX resources_scope_kind_page_idx
    ON control_plane.resources (organization_id, project_id, kind, id)
    WHERE state <> 'DELETED';
CREATE INDEX resources_parent_page_idx
    ON control_plane.resources (organization_id, project_id, parent_id, kind, id)
    WHERE state <> 'DELETED';
CREATE INDEX resources_schedule_due_idx
    ON control_plane.resources (
        organization_id,
        project_id,
        schedule_next_run_at,
        id
    )
    WHERE kind = 'SCHEDULE' AND state = 'ACTIVE';
CREATE INDEX resources_turn_queue_idx
    ON control_plane.resources (organization_id, project_id, created_at, id)
    WHERE kind = 'TURN' AND state = 'QUEUED';
CREATE UNIQUE INDEX resources_project_slug_uidx
    ON control_plane.resources (organization_id, (spec ->> 'slug'))
    WHERE kind = 'PROJECT' AND state <> 'DELETED';
CREATE UNIQUE INDEX resources_stable_key_uidx
    ON control_plane.resources (organization_id, project_id, kind, (spec ->> 'stableKey'))
    WHERE kind IN ('TEAM', 'CHAT', 'ROLE') AND state <> 'DELETED';
CREATE UNIQUE INDEX resources_turn_sequence_uidx
    ON control_plane.resources (
        organization_id,
        project_id,
        ((spec ->> 'sessionId')::uuid),
        ((spec ->> 'sequence')::bigint)
    )
    WHERE kind = 'TURN';
CREATE UNIQUE INDEX resources_runtime_manifest_uidx
    ON control_plane.resources (
        organization_id,
        project_id,
        (spec ->> 'manifestSha256')
    )
    WHERE kind = 'RUNTIME_REVISION' AND state <> 'DELETED';
CREATE UNIQUE INDEX resources_session_conversation_uidx
    ON control_plane.resources (
        organization_id,
        project_id,
        (spec ->> 'conversationId')
    )
    WHERE kind = 'SESSION'
      AND state <> 'DELETED'
      AND coalesce(spec ->> 'conversationId', '') <> '';
CREATE UNIQUE INDEX resources_active_work_claim_uidx
    ON control_plane.resources (
        organization_id,
        project_id,
        ((spec ->> 'processRunId')::uuid),
        ((spec ->> 'turnId')::uuid)
    )
    WHERE kind = 'WORK_CLAIM'
      AND state NOT IN ('ARCHIVED', 'DELETION_PENDING', 'DELETED', 'CANCELLED', 'EXPIRED');
CREATE UNIQUE INDEX resources_artifact_storage_uidx
    ON control_plane.resources (
        organization_id,
        project_id,
        (spec ->> 'storageRef')
    )
    WHERE kind = 'ARTIFACT' AND state <> 'DELETED';

CREATE TABLE control_plane.command_receipts (
    organization_id uuid NOT NULL,
    project_id uuid,
    scope text NOT NULL CHECK (scope ~ '^[a-z][a-z0-9_]{0,63}$'),
    key_hash text NOT NULL CHECK (key_hash ~ '^[a-f0-9]{64}$'),
    request_hash text NOT NULL CHECK (request_hash ~ '^[a-f0-9]{64}$'),
    result jsonb,
    payload jsonb,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, scope, key_hash),
    CHECK (
        (result IS NULL OR jsonb_typeof(result) = 'object')
        AND (payload IS NULL OR jsonb_typeof(payload) IN ('object', 'array'))
        AND octet_length(coalesce(result, '{}'::jsonb)::text) <= 131072
        AND octet_length(coalesce(payload, '{}'::jsonb)::text) <= 131072
    )
);
CREATE INDEX command_receipts_retention_idx
    ON control_plane.command_receipts (created_at, organization_id);

CREATE TABLE control_plane.audit_events (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    action text NOT NULL CHECK (action ~ '^[a-z][a-z0-9_:]{0,95}$'),
    resource_id uuid NOT NULL,
    resource_kind text NOT NULL,
    resource_version bigint NOT NULL,
    outcome text NOT NULL CHECK (outcome IN ('succeeded', 'denied', 'failed')),
    correlation_id uuid NOT NULL,
    policy_revision bigint NOT NULL CHECK (policy_revision BETWEEN 1 AND 9007199254740991),
    occurred_at timestamptz NOT NULL
);
CREATE INDEX audit_events_scope_time_idx
    ON control_plane.audit_events (organization_id, project_id, occurred_at DESC, id);

CREATE TABLE control_plane.outbox_events (
    event_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    event_name text NOT NULL CHECK (
        event_name IN (
            'control_plane.runtime_configuration_changed',
            'control_plane.schedule_changed'
        )
    ),
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    aggregate_version bigint NOT NULL,
    event_sequence bigint NOT NULL,
    correlation_id uuid NOT NULL,
    causation_id uuid,
    envelope jsonb NOT NULL CHECK (
        jsonb_typeof(envelope) = 'object'
        AND octet_length(envelope::text) <= 65536
    ),
    occurred_at timestamptz NOT NULL,
    available_at timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 100),
    lease_owner text,
    lease_token uuid,
    lease_until timestamptz,
    published_at timestamptz,
    last_error_class text,
    terminal boolean NOT NULL DEFAULT false,
    UNIQUE (aggregate_type, aggregate_id, event_sequence),
    CHECK (
        (lease_owner IS NULL AND lease_token IS NULL AND lease_until IS NULL)
        OR (lease_owner IS NOT NULL AND lease_token IS NOT NULL AND lease_until IS NOT NULL)
    )
);
CREATE INDEX outbox_events_claim_idx
    ON control_plane.outbox_events (available_at, occurred_at, event_id)
    WHERE published_at IS NULL AND terminal = false;

CREATE TABLE control_plane.turn_leases (
    turn_id uuid PRIMARY KEY,
    token_hash text NOT NULL CHECK (token_hash ~ '^[a-f0-9]{64}$'),
    workload_id text NOT NULL CHECK (workload_id ~ '^[a-z0-9][a-z0-9._-]{0,127}$'),
    expires_at timestamptz NOT NULL,
    fence bigint NOT NULL CHECK (fence BETWEEN 1 AND 9007199254740991)
);
CREATE INDEX turn_leases_expiry_idx ON control_plane.turn_leases (expires_at);

CREATE TABLE control_plane.schedule_occurrences (
    id uuid PRIMARY KEY,
    schedule_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    scheduled_for timestamptz NOT NULL,
    target_resource_id uuid NOT NULL,
    claimed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (schedule_id, scheduled_for)
);
CREATE INDEX schedule_occurrences_scope_time_idx
    ON control_plane.schedule_occurrences (
        organization_id,
        project_id,
        scheduled_for DESC
    );

CREATE TABLE control_plane.cache_epochs (
    organization_id uuid NOT NULL,
    scope_key text NOT NULL CHECK (
        scope_key = 'tenant'
        OR scope_key ~ '^[a-f0-9-]{36}$'
    ),
    project_id uuid,
    epoch bigint NOT NULL CHECK (epoch BETWEEN 1 AND 9007199254740991),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, scope_key),
    CHECK (
        (scope_key = 'tenant' AND project_id IS NULL)
        OR scope_key = project_id::text
    )
);

CREATE TABLE control_plane.project_actor_permissions (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    permission text NOT NULL CHECK (
        permission = '*'
        OR permission ~ '^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$'
    ),
    source_version bigint NOT NULL CHECK (source_version > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, actor_id, permission)
);
CREATE INDEX project_actor_permissions_actor_idx
    ON control_plane.project_actor_permissions (
        organization_id,
        actor_id,
        project_id
    );

CREATE TABLE control_plane.schema_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version bigint NOT NULL CHECK (version > 0),
    migrated_at timestamptz NOT NULL
);
INSERT INTO control_plane.schema_state (singleton, version, migrated_at)
VALUES (true, 20260731000100, clock_timestamp());

ALTER TABLE control_plane.resources ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.resources FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.command_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.command_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.audit_events FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.outbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.outbox_events FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.turn_leases ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.turn_leases FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.schedule_occurrences ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.schedule_occurrences FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.cache_epochs ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.cache_epochs FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.project_actor_permissions ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.project_actor_permissions FORCE ROW LEVEL SECURITY;

CREATE POLICY resources_runtime_scope ON control_plane.resources
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND (
            project_id = nullif(
                current_setting('mattercodex.project_id', true),
                ''
            )::uuid
            OR (
                nullif(current_setting('mattercodex.project_id', true), '') IS NULL
                AND kind = 'PROJECT'
            )
        )
    )
    WITH CHECK (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND (
            project_id = nullif(
                current_setting('mattercodex.project_id', true),
                ''
            )::uuid
            OR (
                nullif(current_setting('mattercodex.project_id', true), '') IS NULL
                AND kind = 'PROJECT'
                AND project_id = id
            )
        )
    );

CREATE POLICY receipts_runtime_scope ON control_plane.command_receipts
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND project_id IS NOT DISTINCT FROM nullif(
            current_setting('mattercodex.project_id', true),
            ''
        )::uuid
    )
    WITH CHECK (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND project_id IS NOT DISTINCT FROM nullif(
            current_setting('mattercodex.project_id', true),
            ''
        )::uuid
    );

CREATE POLICY audit_runtime_scope ON control_plane.audit_events
    FOR INSERT TO control_plane_runtime
    WITH CHECK (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND (
            project_id = nullif(
                current_setting('mattercodex.project_id', true),
                ''
            )::uuid
            OR nullif(current_setting('mattercodex.project_id', true), '') IS NULL
        )
    );

CREATE POLICY outbox_runtime_insert ON control_plane.outbox_events
    FOR INSERT TO control_plane_runtime
    WITH CHECK (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND (
            project_id = nullif(
                current_setting('mattercodex.project_id', true),
                ''
            )::uuid
            OR nullif(current_setting('mattercodex.project_id', true), '') IS NULL
        )
    );
CREATE POLICY outbox_relay_scope ON control_plane.outbox_events
    FOR ALL TO control_plane_relay
    USING (true)
    WITH CHECK (true);

CREATE POLICY turn_leases_runtime_scope ON control_plane.turn_leases
    FOR ALL TO control_plane_runtime
    USING (
        EXISTS (
            SELECT 1
            FROM control_plane.resources AS resource
            WHERE resource.id = turn_leases.turn_id
        )
    )
    WITH CHECK (
        EXISTS (
            SELECT 1
            FROM control_plane.resources AS resource
            WHERE resource.id = turn_leases.turn_id
        )
    );

CREATE POLICY schedule_occurrences_runtime_scope
    ON control_plane.schedule_occurrences
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND project_id = nullif(
            current_setting('mattercodex.project_id', true),
            ''
        )::uuid
    )
    WITH CHECK (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND project_id = nullif(
            current_setting('mattercodex.project_id', true),
            ''
        )::uuid
    );

CREATE POLICY cache_epochs_runtime_scope ON control_plane.cache_epochs
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND (
            project_id IS NULL
            OR project_id = nullif(
                current_setting('mattercodex.project_id', true),
                ''
            )::uuid
        )
    )
    WITH CHECK (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND (
            project_id IS NULL
            OR project_id = nullif(
                current_setting('mattercodex.project_id', true),
                ''
            )::uuid
        )
    );

CREATE POLICY project_actor_permissions_runtime_read
    ON control_plane.project_actor_permissions
    FOR SELECT TO control_plane_runtime
    USING (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND actor_id = nullif(
            current_setting('mattercodex.actor_id', true),
            ''
        )::uuid
    );
CREATE POLICY project_actor_permissions_runtime_write
    ON control_plane.project_actor_permissions
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND project_id = nullif(
            current_setting('mattercodex.project_id', true),
            ''
        )::uuid
    )
    WITH CHECK (
        organization_id = nullif(
            current_setting('mattercodex.organization_id', true),
            ''
        )::uuid
        AND project_id = nullif(
            current_setting('mattercodex.project_id', true),
            ''
        )::uuid
    );

REVOKE ALL ON ALL TABLES IN SCHEMA control_plane FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON control_plane.resources TO control_plane_runtime;
GRANT SELECT, INSERT ON control_plane.command_receipts TO control_plane_runtime;
GRANT INSERT ON control_plane.audit_events TO control_plane_runtime;
GRANT INSERT ON control_plane.outbox_events TO control_plane_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON control_plane.turn_leases TO control_plane_runtime;
GRANT SELECT, INSERT, UPDATE ON control_plane.schedule_occurrences TO control_plane_runtime;
GRANT SELECT, INSERT, UPDATE ON control_plane.cache_epochs TO control_plane_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE
    ON control_plane.project_actor_permissions TO control_plane_runtime;
GRANT SELECT ON control_plane.schema_state TO control_plane_runtime, control_plane_relay;
GRANT SELECT, UPDATE ON control_plane.outbox_events TO control_plane_relay;
GRANT USAGE ON SEQUENCE control_plane.authority_proof_revision_seq
    TO control_plane_runtime;

RESET ROLE;

-- +goose Down
DROP SCHEMA IF EXISTS control_plane CASCADE;
REVOKE control_plane_runtime FROM PUBLIC;
REVOKE control_plane_relay FROM PUBLIC;
DROP ROLE IF EXISTS control_plane_relay;
DROP ROLE IF EXISTS control_plane_runtime;
DROP ROLE IF EXISTS control_plane_owner;
