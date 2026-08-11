-- +goose Up
RESET ROLE;
SET ROLE integration_gateway_owner;
CREATE SCHEMA IF NOT EXISTS integration_gateway;
CREATE SCHEMA IF NOT EXISTS integration_gateway_extensions;
CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA integration_gateway_extensions;

RESET ROLE;
-- +goose StatementBegin
DO $roles$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'integration_gateway_owner') THEN
        CREATE ROLE integration_gateway_owner NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'integration_gateway_runtime') THEN
        CREATE ROLE integration_gateway_runtime NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'integration_gateway_migrator') THEN
        CREATE ROLE integration_gateway_migrator NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'integration_gateway_role_controller') THEN
        CREATE ROLE integration_gateway_role_controller NOLOGIN;
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
        WHERE rolname IN (
            'integration_gateway_owner',
            'integration_gateway_runtime',
            'integration_gateway_migrator',
            'integration_gateway_role_controller'
        )
          AND (rolsuper OR rolreplication OR rolbypassrls)
    ) THEN
        RAISE EXCEPTION 'integration-gateway managed role has prohibited attributes'
            USING ERRCODE = '42501';
    END IF;
END
$role_safety$;
-- +goose StatementEnd

ALTER ROLE integration_gateway_owner
    NOLOGIN NOCREATEDB NOCREATEROLE NOINHERIT;
ALTER ROLE integration_gateway_runtime
    NOLOGIN NOCREATEDB NOCREATEROLE NOINHERIT;
ALTER ROLE integration_gateway_migrator
    NOLOGIN NOCREATEDB NOCREATEROLE NOINHERIT;
ALTER ROLE integration_gateway_role_controller
    NOLOGIN NOCREATEDB CREATEROLE NOINHERIT;
GRANT pg_signal_backend TO integration_gateway_role_controller;
GRANT integration_gateway_runtime TO integration_gateway_role_controller WITH ADMIN OPTION;
SET ROLE integration_gateway_owner;

CREATE TABLE integration_gateway.runtime_principals (
    principal_name name PRIMARY KEY,
    generation bigint NOT NULL CHECK (generation > 0),
    status text NOT NULL CHECK (status IN ('CURRENT', 'NEXT', 'PREVIOUS', 'RETIRED')),
    not_before timestamptz NOT NULL,
    not_after timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (not_after > not_before)
);

CREATE TABLE integration_gateway.runtime_context_keys (
    key_id text PRIMARY KEY,
    secret bytea NOT NULL CHECK (octet_length(secret) >= 32),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'RETIRED')),
    updated_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX runtime_context_keys_active_uidx
    ON integration_gateway.runtime_context_keys (status) WHERE status = 'ACTIVE';

CREATE TABLE integration_gateway.runtime_transaction_contexts (
    backend_pid integer NOT NULL,
    transaction_id bigint NOT NULL,
    principal_name name NOT NULL REFERENCES integration_gateway.runtime_principals(principal_name),
    principal_generation bigint NOT NULL,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    actor_id text NOT NULL,
    nonce uuid NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (backend_pid, transaction_id)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.bootstrap_runtime_principal(
    requested_principal_name text,
    requested_generation bigint,
    requested_password text
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
DECLARE
    role_exists boolean;
    role_safe boolean;
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_migrator', 'member')
       OR requested_principal_name <> ('integration_gateway_runtime_g' || requested_generation::text)
       OR requested_generation <= 0
       OR length(requested_password) < 32 OR length(requested_password) > 1024
       OR requested_password <> btrim(requested_password) THEN
        RAISE EXCEPTION 'runtime principal bootstrap input is invalid' USING ERRCODE = '28000';
    END IF;
    IF EXISTS (
        SELECT 1 FROM integration_gateway.runtime_principals
         WHERE principal_name::text = requested_principal_name AND status = 'RETIRED'
    ) THEN
        RAISE EXCEPTION 'retired runtime principal cannot be reactivated' USING ERRCODE = '28000';
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
     WHERE role.rolname = requested_principal_name;
    IF coalesce(role_exists, false) AND NOT role_safe THEN
        RAISE EXCEPTION 'existing runtime principal role is unsafe'
            USING ERRCODE = '42501';
    END IF;
    IF coalesce(role_exists, false) THEN
        EXECUTE format(
            'ALTER ROLE %I LOGIN PASSWORD %L NOCREATEDB NOCREATEROLE INHERIT',
            requested_principal_name,
            requested_password
        );
    ELSE
        EXECUTE format(
            'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS',
            requested_principal_name,
            requested_password
        );
    END IF;
    EXECUTE format('GRANT integration_gateway_runtime TO %I', requested_principal_name);
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.retire_runtime_principal(
    requested_principal_name text
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_migrator', 'member')
       OR requested_principal_name !~ '^integration_gateway_runtime_g[1-9][0-9]*$'
       OR NOT EXISTS (
           SELECT 1 FROM integration_gateway.runtime_principals
            WHERE principal_name::text = requested_principal_name
       ) THEN
        RAISE EXCEPTION 'runtime principal retirement input is invalid' USING ERRCODE = '28000';
    END IF;
    EXECUTE format('ALTER ROLE %I NOLOGIN', requested_principal_name);
    EXECUTE format('REVOKE integration_gateway_runtime FROM %I', requested_principal_name);
    PERFORM pg_terminate_backend(activity.pid)
      FROM pg_catalog.pg_stat_activity AS activity
     WHERE activity.usename = requested_principal_name AND activity.pid <> pg_backend_pid();
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.activate_runtime_context(
    requested_tenant_id text,
    requested_project_id text,
    requested_actor_id text,
    requested_principal_name name,
    requested_principal_generation bigint,
    requested_key_id text,
    requested_nonce uuid,
    requested_expires_unix_micro bigint,
    requested_signature bytea
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway, integration_gateway_extensions
AS $function$
DECLARE
    active_secret bytea;
    canonical text;
    context_expires_at timestamptz;
BEGIN
    context_expires_at := to_timestamp(requested_expires_unix_micro::numeric / 1000000);
    IF requested_principal_name::text <> session_user
       OR requested_tenant_id = '' OR requested_project_id = '' OR requested_actor_id = ''
       OR requested_expires_unix_micro <= floor(extract(epoch FROM clock_timestamp()) * 1000000)
       OR requested_expires_unix_micro > floor(extract(epoch FROM clock_timestamp() + interval '10 seconds') * 1000000)
       OR NOT pg_has_role(session_user, 'integration_gateway_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime context identity is invalid' USING ERRCODE = '28000';
    END IF;

    SELECT context_key.secret INTO active_secret
      FROM integration_gateway.runtime_context_keys AS context_key
     WHERE context_key.key_id = requested_key_id AND context_key.status = 'ACTIVE'
     FOR SHARE;
    IF active_secret IS NULL THEN
        RAISE EXCEPTION 'runtime context key is unavailable' USING ERRCODE = '28000';
    END IF;

    PERFORM 1
      FROM integration_gateway.runtime_principals AS principal
      JOIN pg_catalog.pg_roles AS role ON role.rolname = principal.principal_name
     WHERE principal.principal_name = requested_principal_name
       AND principal.generation = requested_principal_generation
       AND principal.status IN ('CURRENT', 'NEXT', 'PREVIOUS')
       AND clock_timestamp() >= principal.not_before AND clock_timestamp() < principal.not_after
       AND role.rolcanlogin AND NOT role.rolsuper AND NOT role.rolbypassrls
       AND pg_has_role(role.rolname, 'integration_gateway_runtime', 'member')
     FOR SHARE OF principal;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime principal is not active' USING ERRCODE = '28000';
    END IF;

    canonical := 'v1' || chr(10) || requested_principal_name::text || chr(10)
        || requested_principal_generation::text || chr(10) || requested_tenant_id || chr(10)
        || requested_project_id || chr(10) || requested_actor_id || chr(10)
        || requested_nonce::text || chr(10) || requested_expires_unix_micro::text;
    IF integration_gateway_extensions.hmac(convert_to(canonical, 'UTF8'), active_secret, 'sha256') <> requested_signature THEN
        RAISE EXCEPTION 'runtime context signature is invalid' USING ERRCODE = '28000';
    END IF;

    DELETE FROM integration_gateway.runtime_transaction_contexts
     WHERE ctid IN (
        SELECT expired.ctid FROM integration_gateway.runtime_transaction_contexts AS expired
         WHERE expired.expires_at < clock_timestamp() - interval '1 minute'
         ORDER BY expired.expires_at LIMIT 1000
     );
    INSERT INTO integration_gateway.runtime_transaction_contexts (
        backend_pid, transaction_id, principal_name, principal_generation,
        tenant_id, project_id, actor_id, nonce, expires_at, created_at
    ) VALUES (
        pg_backend_pid(), txid_current(), requested_principal_name, requested_principal_generation,
        requested_tenant_id, requested_project_id, requested_actor_id, requested_nonce,
        context_expires_at, clock_timestamp()
    );
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.runtime_scope(
    OUT tenant_id text, OUT project_id text, OUT actor_id text
) RETURNS record
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    SELECT context.tenant_id, context.project_id, context.actor_id
      INTO tenant_id, project_id, actor_id
      FROM integration_gateway.runtime_transaction_contexts AS context
      JOIN integration_gateway.runtime_principals AS principal
        ON principal.principal_name = context.principal_name
       AND principal.generation = context.principal_generation
     WHERE context.backend_pid = pg_backend_pid() AND context.transaction_id = txid_current()
       AND context.principal_name::text = session_user AND context.expires_at > clock_timestamp()
       AND principal.status IN ('CURRENT', 'NEXT', 'PREVIOUS')
       AND clock_timestamp() >= principal.not_before AND clock_timestamp() < principal.not_after
       AND pg_has_role(session_user, 'integration_gateway_runtime', 'member');
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime context is not active' USING ERRCODE = '28000';
    END IF;
END
$function$;
-- +goose StatementEnd

CREATE TABLE integration_gateway.definitions (
    definition_id text NOT NULL,
    definition_version bigint NOT NULL CHECK (definition_version > 0),
    canonical_digest text NOT NULL CHECK (canonical_digest ~ '^[0-9a-f]{64}$'),
    source bytea NOT NULL CHECK (octet_length(source) BETWEEN 1 AND 262144),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (definition_id, definition_version)
);

CREATE TABLE integration_gateway.connections (
    connection_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    integration_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    generation bigint NOT NULL CHECK (generation > 0),
    status text NOT NULL CHECK (status IN ('PENDING', 'VALID', 'INVALID', 'REVOKED')),
    definition_id text NOT NULL,
    definition_version bigint NOT NULL,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    updated_at timestamptz NOT NULL,
    FOREIGN KEY (definition_id, definition_version)
        REFERENCES integration_gateway.definitions(definition_id, definition_version)
);
CREATE INDEX connections_scope_idx ON integration_gateway.connections(tenant_id, project_id, integration_id);

CREATE TABLE integration_gateway.grants (
    grant_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    session_id text NOT NULL,
    turn_id text NOT NULL,
    attempt integer NOT NULL CHECK (attempt > 0),
    input_digest text NOT NULL CHECK (input_digest ~ '^[0-9a-f]{64}$'),
    runtime_revision_id text NOT NULL,
    connection_id text NOT NULL REFERENCES integration_gateway.connections(connection_id),
    generation bigint NOT NULL CHECK (generation > 0),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'REVOKED', 'EXPIRED')),
    expires_at timestamptz NOT NULL,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    updated_at timestamptz NOT NULL
);
CREATE INDEX grants_session_idx ON integration_gateway.grants(tenant_id, project_id, session_id, turn_id, attempt);

CREATE TABLE integration_gateway.transport_sessions (
    transport_session_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    agent_session_id text NOT NULL,
    turn_id text NOT NULL,
    attempt integer NOT NULL CHECK (attempt > 0),
    input_digest text NOT NULL CHECK (input_digest ~ '^[0-9a-f]{64}$'),
    runtime_revision_id text NOT NULL,
    grant_generation bigint NOT NULL CHECK (grant_generation > 0),
    token_digest text NOT NULL CHECK (token_digest ~ '^[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('INITIALIZING', 'ACTIVE', 'CLOSED', 'EXPIRED')),
    request_count bigint NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    concurrent_requests integer NOT NULL DEFAULT 0 CHECK (concurrent_requests >= 0),
    expires_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL
);
CREATE INDEX transport_sessions_expiry_idx ON integration_gateway.transport_sessions(status, expires_at);

CREATE TABLE integration_gateway.invocations (
    invocation_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    transport_session_id text NOT NULL REFERENCES integration_gateway.transport_sessions(transport_session_id),
    agent_session_id text NOT NULL,
    turn_id text NOT NULL,
    attempt integer NOT NULL CHECK (attempt > 0),
    connection_id text NOT NULL REFERENCES integration_gateway.connections(connection_id),
    connection_generation bigint NOT NULL,
    grant_id text NOT NULL REFERENCES integration_gateway.grants(grant_id),
    grant_generation bigint NOT NULL,
    semantic_key text NOT NULL,
    canonical_request_hash text NOT NULL CHECK (canonical_request_hash ~ '^[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('PENDING_APPROVAL', 'APPROVED', 'REJECTED', 'EXECUTING', 'SUCCEEDED', 'FAILED', 'UNKNOWN', 'CANCELLED', 'EXPIRED')),
    expires_at timestamptz NOT NULL,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id, project_id, transport_session_id, semantic_key)
);
CREATE INDEX invocations_claim_idx ON integration_gateway.invocations(status, created_at);
CREATE UNIQUE INDEX invocations_open_turn_uidx
    ON integration_gateway.invocations(tenant_id, project_id, agent_session_id, turn_id, attempt)
    WHERE status IN ('PENDING_APPROVAL', 'APPROVED', 'EXECUTING');

CREATE TABLE integration_gateway.approvals (
    approval_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    invocation_id text NOT NULL UNIQUE REFERENCES integration_gateway.invocations(invocation_id),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'CANCELLED', 'EXPIRED')),
    expires_at timestamptz NOT NULL,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    decided_at timestamptz
);

CREATE TABLE integration_gateway.execution_attempts (
    attempt_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    invocation_id text NOT NULL REFERENCES integration_gateway.invocations(invocation_id),
    attempt_number integer NOT NULL CHECK (attempt_number > 0),
    fence bigint NOT NULL CHECK (fence > 0),
    connection_generation bigint NOT NULL,
    grant_generation bigint NOT NULL,
    provider_idempotency_key text NOT NULL,
    outcome text NOT NULL,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    started_at timestamptz NOT NULL,
    finished_at timestamptz,
    UNIQUE (invocation_id, attempt_number),
    UNIQUE (invocation_id, fence)
);

CREATE TABLE integration_gateway.results (
    invocation_id text PRIMARY KEY REFERENCES integration_gateway.invocations(invocation_id),
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    attempt_id text NOT NULL UNIQUE REFERENCES integration_gateway.execution_attempts(attempt_id),
    status text NOT NULL CHECK (status IN ('SUCCEEDED', 'FAILED', 'UNKNOWN')),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    completed_at timestamptz NOT NULL
);

CREATE TABLE integration_gateway.idempotency_receipts (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    key_hash text NOT NULL CHECK (key_hash ~ '^[0-9a-f]{64}$'),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    invocation_id text NOT NULL REFERENCES integration_gateway.invocations(invocation_id),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_id, key_hash)
);

CREATE TABLE integration_gateway.audit_events (
    audit_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    actor_id text NOT NULL,
    action text NOT NULL,
    resource_kind text NOT NULL,
    resource_id text NOT NULL,
    request_hash text NOT NULL DEFAULT '',
    outcome text NOT NULL,
    reason_code text NOT NULL DEFAULT '',
    occurred_at timestamptz NOT NULL
);
CREATE INDEX audit_events_scope_idx ON integration_gateway.audit_events(tenant_id, project_id, occurred_at DESC);

-- Эти таблицы не содержат business payload и недоступны runtime-роли напрямую.
-- Они дают worker только scope реально ожидающей работы, после чего все чтения и
-- переходы снова выполняются через signed transaction context и FORCE RLS.
CREATE TABLE integration_gateway.execution_work_scopes (
    invocation_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    available_at timestamptz NOT NULL
);
CREATE INDEX execution_work_scopes_order_idx
    ON integration_gateway.execution_work_scopes(available_at, invocation_id);

CREATE TABLE integration_gateway.lifecycle_work_scopes (
    work_kind text NOT NULL CHECK (work_kind IN ('TRANSPORT_SESSION', 'APPROVAL', 'INVOCATION')),
    work_id text NOT NULL,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    due_at timestamptz NOT NULL,
    PRIMARY KEY (work_kind, work_id)
);
CREATE INDEX lifecycle_work_scopes_order_idx
    ON integration_gateway.lifecycle_work_scopes(due_at, work_kind, work_id);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.sync_execution_work_scope()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    IF NEW.status = 'APPROVED' AND NEW.expires_at > clock_timestamp() THEN
        INSERT INTO integration_gateway.execution_work_scopes (
            invocation_id, tenant_id, project_id, available_at
        ) VALUES (NEW.invocation_id, NEW.tenant_id, NEW.project_id, NEW.updated_at)
        ON CONFLICT (invocation_id) DO UPDATE SET
            tenant_id = EXCLUDED.tenant_id,
            project_id = EXCLUDED.project_id,
            available_at = EXCLUDED.available_at;
    ELSE
        DELETE FROM integration_gateway.execution_work_scopes
         WHERE invocation_id = NEW.invocation_id;
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd

CREATE TRIGGER invocations_sync_execution_work_scope
AFTER INSERT OR UPDATE OF status, expires_at ON integration_gateway.invocations
FOR EACH ROW EXECUTE FUNCTION integration_gateway.sync_execution_work_scope();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.sync_lifecycle_work_scope()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
DECLARE
    selected_kind text;
    selected_id text;
BEGIN
    IF TG_TABLE_NAME = 'transport_sessions' THEN
        selected_kind := 'TRANSPORT_SESSION';
        selected_id := NEW.transport_session_id;
        IF NEW.status IN ('INITIALIZING', 'ACTIVE') THEN
            INSERT INTO integration_gateway.lifecycle_work_scopes (
                work_kind, work_id, tenant_id, project_id, due_at
            ) VALUES (selected_kind, selected_id, NEW.tenant_id, NEW.project_id, NEW.expires_at)
            ON CONFLICT (work_kind, work_id) DO UPDATE SET
                tenant_id = EXCLUDED.tenant_id,
                project_id = EXCLUDED.project_id,
                due_at = EXCLUDED.due_at;
        ELSE
            DELETE FROM integration_gateway.lifecycle_work_scopes
             WHERE work_kind = selected_kind AND work_id = selected_id;
        END IF;
    ELSIF TG_TABLE_NAME = 'approvals' THEN
        selected_kind := 'APPROVAL';
        selected_id := NEW.approval_id;
        IF NEW.status = 'PENDING' THEN
            INSERT INTO integration_gateway.lifecycle_work_scopes (
                work_kind, work_id, tenant_id, project_id, due_at
            ) VALUES (selected_kind, selected_id, NEW.tenant_id, NEW.project_id, NEW.expires_at)
            ON CONFLICT (work_kind, work_id) DO UPDATE SET
                tenant_id = EXCLUDED.tenant_id,
                project_id = EXCLUDED.project_id,
                due_at = EXCLUDED.due_at;
        ELSE
            DELETE FROM integration_gateway.lifecycle_work_scopes
             WHERE work_kind = selected_kind AND work_id = selected_id;
        END IF;
    ELSIF TG_TABLE_NAME = 'invocations' THEN
        selected_kind := 'INVOCATION';
        selected_id := NEW.invocation_id;
        IF NEW.status = 'EXECUTING' THEN
            INSERT INTO integration_gateway.lifecycle_work_scopes (
                work_kind, work_id, tenant_id, project_id, due_at
            ) VALUES (selected_kind, selected_id, NEW.tenant_id, NEW.project_id, NEW.updated_at + interval '1 minute')
            ON CONFLICT (work_kind, work_id) DO UPDATE SET
                tenant_id = EXCLUDED.tenant_id,
                project_id = EXCLUDED.project_id,
                due_at = EXCLUDED.due_at;
        ELSIF NEW.status = 'APPROVED' THEN
            INSERT INTO integration_gateway.lifecycle_work_scopes (
                work_kind, work_id, tenant_id, project_id, due_at
            ) VALUES (selected_kind, selected_id, NEW.tenant_id, NEW.project_id, NEW.expires_at)
            ON CONFLICT (work_kind, work_id) DO UPDATE SET
                tenant_id = EXCLUDED.tenant_id,
                project_id = EXCLUDED.project_id,
                due_at = EXCLUDED.due_at;
        ELSE
            DELETE FROM integration_gateway.lifecycle_work_scopes
             WHERE work_kind = selected_kind AND work_id = selected_id;
        END IF;
    ELSE
        RAISE EXCEPTION 'unsupported lifecycle work table' USING ERRCODE = '22023';
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd

CREATE TRIGGER sessions_sync_lifecycle_work_scope
AFTER INSERT OR UPDATE OF status, expires_at ON integration_gateway.transport_sessions
FOR EACH ROW EXECUTE FUNCTION integration_gateway.sync_lifecycle_work_scope();

CREATE TRIGGER approvals_sync_lifecycle_work_scope
AFTER INSERT OR UPDATE OF status, expires_at ON integration_gateway.approvals
FOR EACH ROW EXECUTE FUNCTION integration_gateway.sync_lifecycle_work_scope();

CREATE TRIGGER invocations_sync_lifecycle_work_scope
AFTER INSERT OR UPDATE OF status, expires_at, updated_at ON integration_gateway.invocations
FOR EACH ROW EXECUTE FUNCTION integration_gateway.sync_lifecycle_work_scope();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.next_execution_scope(
    OUT tenant_id text, OUT project_id text
) RETURNS record
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime identity is invalid' USING ERRCODE = '28000';
    END IF;
    PERFORM 1 FROM integration_gateway.runtime_principals AS principal
     WHERE principal.principal_name::text = session_user
       AND principal.status IN ('CURRENT', 'NEXT', 'PREVIOUS')
       AND clock_timestamp() >= principal.not_before AND clock_timestamp() < principal.not_after;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime principal is not active' USING ERRCODE = '28000';
    END IF;
    SELECT scope.tenant_id, scope.project_id INTO tenant_id, project_id
      FROM integration_gateway.execution_work_scopes AS scope
     ORDER BY scope.available_at, scope.invocation_id
     LIMIT 1;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.next_lifecycle_scope(
    OUT tenant_id text, OUT project_id text
) RETURNS record
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime identity is invalid' USING ERRCODE = '28000';
    END IF;
    PERFORM 1 FROM integration_gateway.runtime_principals AS principal
     WHERE principal.principal_name::text = session_user
       AND principal.status IN ('CURRENT', 'NEXT', 'PREVIOUS')
       AND clock_timestamp() >= principal.not_before AND clock_timestamp() < principal.not_after;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime principal is not active' USING ERRCODE = '28000';
    END IF;
    SELECT scope.tenant_id, scope.project_id INTO tenant_id, project_id
      FROM integration_gateway.lifecycle_work_scopes AS scope
     WHERE scope.due_at <= clock_timestamp()
     ORDER BY scope.due_at, scope.work_kind, scope.work_id
     LIMIT 1;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.runtime_identity_ready()
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_runtime', 'member') THEN
        RETURN false;
    END IF;
    RETURN EXISTS (
        SELECT 1 FROM integration_gateway.runtime_principals AS principal
         WHERE principal.principal_name::text = session_user
           AND principal.status IN ('CURRENT', 'NEXT', 'PREVIOUS')
           AND clock_timestamp() >= principal.not_before
           AND clock_timestamp() < principal.not_after
    );
END
$function$;
-- +goose StatementEnd

ALTER TABLE integration_gateway.connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.connections FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.grants FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.transport_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.transport_sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.invocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.invocations FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.approvals FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.execution_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.execution_attempts FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.results ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.results FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.idempotency_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.idempotency_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.audit_events FORCE ROW LEVEL SECURITY;

CREATE POLICY connections_runtime_scope ON integration_gateway.connections
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY grants_runtime_scope ON integration_gateway.grants
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY sessions_runtime_scope ON integration_gateway.transport_sessions
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY invocations_runtime_scope ON integration_gateway.invocations
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY approvals_runtime_scope ON integration_gateway.approvals
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY attempts_runtime_scope ON integration_gateway.execution_attempts
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY results_runtime_scope ON integration_gateway.results
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY receipts_runtime_scope ON integration_gateway.idempotency_receipts
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY audit_runtime_scope ON integration_gateway.audit_events
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));

REVOKE ALL ON SCHEMA integration_gateway FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA integration_gateway FROM PUBLIC;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA integration_gateway FROM PUBLIC;
REVOKE ALL ON SCHEMA integration_gateway_extensions FROM PUBLIC;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA integration_gateway_extensions FROM PUBLIC;
REVOKE ALL ON integration_gateway.execution_work_scopes FROM integration_gateway_runtime;
REVOKE ALL ON integration_gateway.lifecycle_work_scopes FROM integration_gateway_runtime;
GRANT USAGE ON SCHEMA integration_gateway TO integration_gateway_runtime;
GRANT USAGE ON SCHEMA integration_gateway TO integration_gateway_owner;
GRANT USAGE ON SCHEMA integration_gateway TO integration_gateway_migrator;
GRANT USAGE ON SCHEMA integration_gateway TO integration_gateway_role_controller;
GRANT USAGE ON SCHEMA integration_gateway_extensions TO integration_gateway_owner;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA integration_gateway_extensions TO integration_gateway_owner;
GRANT USAGE ON SCHEMA integration_gateway_extensions TO integration_gateway_runtime;
GRANT EXECUTE ON FUNCTION integration_gateway_extensions.gen_random_uuid() TO integration_gateway_runtime;
GRANT SELECT, INSERT ON integration_gateway.definitions TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.connections TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.grants TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.transport_sessions TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.invocations TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.approvals TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.execution_attempts TO integration_gateway_runtime;
GRANT SELECT, INSERT ON integration_gateway.results TO integration_gateway_runtime;
GRANT SELECT, INSERT ON integration_gateway.idempotency_receipts TO integration_gateway_runtime;
GRANT SELECT, INSERT ON integration_gateway.audit_events TO integration_gateway_runtime;
GRANT EXECUTE ON FUNCTION integration_gateway.activate_runtime_context(text, text, text, name, bigint, text, uuid, bigint, bytea) TO integration_gateway_runtime;
GRANT EXECUTE ON FUNCTION integration_gateway.runtime_scope() TO integration_gateway_runtime;
GRANT EXECUTE ON FUNCTION integration_gateway.next_execution_scope() TO integration_gateway_runtime;
GRANT EXECUTE ON FUNCTION integration_gateway.next_lifecycle_scope() TO integration_gateway_runtime;
GRANT EXECUTE ON FUNCTION integration_gateway.runtime_identity_ready() TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.runtime_principals TO integration_gateway_migrator;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.runtime_context_keys TO integration_gateway_migrator;
GRANT SELECT ON integration_gateway.runtime_principals TO integration_gateway_role_controller;
GRANT EXECUTE ON FUNCTION integration_gateway.bootstrap_runtime_principal(text, bigint, text)
    TO integration_gateway_migrator;
GRANT EXECUTE ON FUNCTION integration_gateway.retire_runtime_principal(text)
    TO integration_gateway_migrator;

ALTER TABLE integration_gateway.runtime_principals OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.runtime_context_keys OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.runtime_transaction_contexts OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.definitions OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.connections OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.grants OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.transport_sessions OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.invocations OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.approvals OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.execution_attempts OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.results OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.idempotency_receipts OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.audit_events OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.execution_work_scopes OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.lifecycle_work_scopes OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.activate_runtime_context(text, text, text, name, bigint, text, uuid, bigint, bytea)
    OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.bootstrap_runtime_principal(text, bigint, text)
    OWNER TO integration_gateway_role_controller;
ALTER FUNCTION integration_gateway.retire_runtime_principal(text)
    OWNER TO integration_gateway_role_controller;
ALTER FUNCTION integration_gateway.runtime_scope() OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.sync_execution_work_scope() OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.sync_lifecycle_work_scope() OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.next_execution_scope() OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.next_lifecycle_scope() OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.runtime_identity_ready() OWNER TO integration_gateway_owner;

-- +goose Down
-- Forward-only migration: откат выполняется возвратом предыдущего image с
-- сохранением авторитетных approval и execution receipts.
SELECT 1;
