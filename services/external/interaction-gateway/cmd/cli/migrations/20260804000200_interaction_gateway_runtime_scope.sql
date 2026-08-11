-- +goose Up
RESET ROLE;
SET ROLE interaction_gateway_owner;
CREATE SCHEMA IF NOT EXISTS interaction_gateway_security;
CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA interaction_gateway_security;

CREATE TABLE interaction_gateway_runtime_credential_fence (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    high_watermark_generation bigint NOT NULL CHECK (high_watermark_generation > 0),
    served_generation bigint NOT NULL CHECK (served_generation > 0),
    context_key_id text NOT NULL CHECK (length(context_key_id) BETWEEN 1 AND 128),
    context_key_digest text NOT NULL CHECK (context_key_digest ~ '^[0-9a-f]{64}$'),
    updated_at timestamptz NOT NULL,
    CHECK (served_generation = high_watermark_generation)
);

CREATE TABLE interaction_gateway_runtime_principals (
    principal_name name PRIMARY KEY,
    generation bigint NOT NULL UNIQUE CHECK (generation > 0),
    status text NOT NULL CHECK (status IN ('CURRENT', 'PREVIOUS', 'RETIRED')),
    not_before timestamptz NOT NULL,
    not_after timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (not_after > not_before)
);

CREATE TABLE interaction_gateway_runtime_context_keys (
    key_id text PRIMARY KEY,
    secret bytea NOT NULL CHECK (octet_length(secret) >= 32),
    digest_sha256 text NOT NULL CHECK (digest_sha256 ~ '^[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'RETIRED')),
    updated_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX interaction_gateway_runtime_context_key_active_uidx
    ON interaction_gateway_runtime_context_keys (status) WHERE status = 'ACTIVE';

CREATE TABLE interaction_gateway_runtime_transaction_contexts (
    backend_pid integer NOT NULL,
    transaction_id bigint NOT NULL,
    principal_name name NOT NULL REFERENCES interaction_gateway_runtime_principals(principal_name),
    principal_generation bigint NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id text NOT NULL CHECK (length(actor_id) BETWEEN 1 AND 256),
    nonce uuid NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (backend_pid, transaction_id)
);

-- Эти индексы не содержат payload. Они позволяют bounded worker узнать только
-- authority scope следующей работы до открытия signed scoped transaction.
CREATE TABLE interaction_gateway_inbound_work_scopes (
    inbound_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    due_at timestamptz NOT NULL
);
CREATE TABLE interaction_gateway_delivery_work_scopes (
    delivery_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    provider_post_id text NOT NULL DEFAULT '',
    delivery_active boolean NOT NULL DEFAULT true,
    owner_gate_active boolean NOT NULL DEFAULT false,
    due_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX interaction_gateway_delivery_scope_post_uidx
    ON interaction_gateway_delivery_work_scopes(provider_post_id) WHERE provider_post_id <> '';
CREATE TABLE interaction_gateway_turn_watch_work_scopes (
    turn_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    due_at timestamptz NOT NULL
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_reconcile_runtime_identity(
    requested_generation bigint,
    requested_key_id text,
    requested_context_key bytea
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public, interaction_gateway_security
AS $function$
DECLARE
    requested_principal name := ('interaction_gateway_runtime_g' || requested_generation::text)::name;
    current_high_watermark bigint;
    requested_digest text;
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_owner', 'member')
       OR requested_generation <= 0 OR length(requested_key_id) NOT BETWEEN 1 AND 128
       OR octet_length(requested_context_key) < 32 THEN
        RAISE EXCEPTION 'runtime identity reconciliation input is invalid' USING ERRCODE = '28000';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_roles
         WHERE rolname = requested_principal::text AND rolcanlogin
           AND NOT rolsuper AND NOT rolbypassrls
    ) THEN
        RAISE EXCEPTION 'runtime principal is unavailable' USING ERRCODE = '28000';
    END IF;
    SELECT high_watermark_generation INTO current_high_watermark
      FROM interaction_gateway_runtime_credential_fence WHERE singleton FOR UPDATE;
    IF current_high_watermark IS NOT NULL AND requested_generation < current_high_watermark THEN
        RAISE EXCEPTION 'runtime credential rollback is forbidden' USING ERRCODE = '28000';
    END IF;
    IF EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals
         WHERE generation = requested_generation AND status = 'RETIRED'
    ) THEN
        RAISE EXCEPTION 'retired runtime credential cannot be restored' USING ERRCODE = '28000';
    END IF;
    IF current_high_watermark IS NOT NULL
       AND requested_generation > current_high_watermark
       AND EXISTS (
           SELECT 1 FROM interaction_gateway_runtime_principals
            WHERE status = 'PREVIOUS'
       ) THEN
        RAISE EXCEPTION 'previous runtime credential must be retired before rotation' USING ERRCODE = '28000';
    END IF;
    requested_digest := encode(interaction_gateway_security.digest(requested_context_key, 'sha256'), 'hex');
    IF EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_context_keys
         WHERE key_id = requested_key_id AND status = 'RETIRED'
    ) THEN
        RAISE EXCEPTION 'retired runtime context key cannot be restored' USING ERRCODE = '28000';
    END IF;

    UPDATE interaction_gateway_runtime_principals
       SET status = CASE WHEN generation = requested_generation THEN 'CURRENT' ELSE 'PREVIOUS' END,
           updated_at = clock_timestamp()
     WHERE status IN ('CURRENT', 'PREVIOUS');
    INSERT INTO interaction_gateway_runtime_principals (
        principal_name, generation, status, not_before, not_after, updated_at
    ) VALUES (
        requested_principal, requested_generation, 'CURRENT',
        clock_timestamp() - interval '5 minutes', clock_timestamp() + interval '400 days', clock_timestamp()
    ) ON CONFLICT (principal_name) DO UPDATE SET
        status = 'CURRENT', not_after = GREATEST(interaction_gateway_runtime_principals.not_after, EXCLUDED.not_after),
        updated_at = EXCLUDED.updated_at
      WHERE interaction_gateway_runtime_principals.generation = EXCLUDED.generation
        AND interaction_gateway_runtime_principals.status <> 'RETIRED';

    UPDATE interaction_gateway_runtime_context_keys
       SET status = 'RETIRED', secret = interaction_gateway_security.gen_random_bytes(32), updated_at = clock_timestamp()
     WHERE status = 'ACTIVE' AND (key_id <> requested_key_id OR digest_sha256 <> requested_digest);
    INSERT INTO interaction_gateway_runtime_context_keys (key_id, secret, digest_sha256, status, updated_at)
    VALUES (requested_key_id, requested_context_key, requested_digest, 'ACTIVE', clock_timestamp())
    ON CONFLICT (key_id) DO UPDATE SET updated_at = EXCLUDED.updated_at
      WHERE interaction_gateway_runtime_context_keys.status = 'ACTIVE'
        AND interaction_gateway_runtime_context_keys.digest_sha256 = EXCLUDED.digest_sha256;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime context key replacement is invalid' USING ERRCODE = '28000';
    END IF;

    INSERT INTO interaction_gateway_runtime_credential_fence (
        singleton, high_watermark_generation, served_generation, context_key_id, context_key_digest, updated_at
    ) VALUES (true, requested_generation, requested_generation, requested_key_id, requested_digest, clock_timestamp())
    ON CONFLICT (singleton) DO UPDATE SET
        high_watermark_generation = EXCLUDED.high_watermark_generation,
        served_generation = EXCLUDED.served_generation,
        context_key_id = EXCLUDED.context_key_id,
        context_key_digest = EXCLUDED.context_key_digest,
        updated_at = EXCLUDED.updated_at
      WHERE interaction_gateway_runtime_credential_fence.high_watermark_generation <= EXCLUDED.high_watermark_generation;

    EXECUTE format('GRANT interaction_gateway_runtime TO %I', requested_principal);
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_retire_runtime_identity(requested_generation bigint)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $function$
DECLARE
    requested_principal name := ('interaction_gateway_runtime_g' || requested_generation::text)::name;
    current_high_watermark bigint;
BEGIN
    IF session_user <> 'interaction_gateway_migrator' THEN
        RAISE EXCEPTION 'runtime identity retirement is forbidden' USING ERRCODE = '28000';
    END IF;
    SELECT high_watermark_generation INTO STRICT current_high_watermark
      FROM interaction_gateway_runtime_credential_fence WHERE singleton FOR UPDATE;
    IF requested_generation >= current_high_watermark OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals
         WHERE generation = requested_generation AND status <> 'RETIRED'
    ) THEN
        RAISE EXCEPTION 'runtime identity retirement input is invalid' USING ERRCODE = '28000';
    END IF;
    UPDATE interaction_gateway_runtime_principals
       SET status = 'RETIRED', updated_at = clock_timestamp() WHERE generation = requested_generation;
    EXECUTE format('ALTER ROLE %I NOLOGIN', requested_principal);
    EXECUTE format('REVOKE interaction_gateway_runtime FROM %I', requested_principal);
    PERFORM pg_terminate_backend(pid) FROM pg_catalog.pg_stat_activity
     WHERE usename = requested_principal::text AND pid <> pg_backend_pid();
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_activate_runtime_context(
    requested_organization_id uuid,
    requested_project_id uuid,
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
SET search_path = pg_catalog, public, interaction_gateway_security
AS $function$
DECLARE
    active_secret bytea;
    canonical text;
    context_expires_at timestamptz;
BEGIN
    context_expires_at := to_timestamp(requested_expires_unix_micro::numeric / 1000000);
    IF requested_principal_name::text <> session_user OR requested_actor_id = ''
       OR requested_expires_unix_micro <= floor(extract(epoch FROM clock_timestamp()) * 1000000)
       OR requested_expires_unix_micro > floor(extract(epoch FROM clock_timestamp() + interval '10 seconds') * 1000000)
       OR NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime context identity is invalid' USING ERRCODE = '28000';
    END IF;
    SELECT secret INTO active_secret FROM interaction_gateway_runtime_context_keys
     WHERE key_id = requested_key_id AND status = 'ACTIVE' FOR SHARE;
    IF active_secret IS NULL THEN
        RAISE EXCEPTION 'runtime context key is unavailable' USING ERRCODE = '28000';
    END IF;
    PERFORM 1 FROM interaction_gateway_runtime_principals AS principal
      JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
      JOIN pg_catalog.pg_roles AS role ON role.rolname = principal.principal_name
     WHERE principal.principal_name = requested_principal_name
       AND principal.generation = requested_principal_generation
       AND principal.generation = fence.served_generation
       AND principal.status = 'CURRENT'
       AND clock_timestamp() >= principal.not_before AND clock_timestamp() < principal.not_after
       AND role.rolcanlogin AND NOT role.rolsuper AND NOT role.rolbypassrls
       AND pg_has_role(role.rolname, 'interaction_gateway_runtime', 'member') FOR SHARE OF principal;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime principal is not active' USING ERRCODE = '28000';
    END IF;
    canonical := 'v1' || chr(10) || requested_principal_name::text || chr(10)
        || requested_principal_generation::text || chr(10) || requested_organization_id::text || chr(10)
        || requested_project_id::text || chr(10) || requested_actor_id || chr(10)
        || requested_nonce::text || chr(10) || requested_expires_unix_micro::text;
    IF interaction_gateway_security.hmac(convert_to(canonical, 'UTF8'), active_secret, 'sha256') <> requested_signature THEN
        RAISE EXCEPTION 'runtime context signature is invalid' USING ERRCODE = '28000';
    END IF;
    DELETE FROM interaction_gateway_runtime_transaction_contexts
     WHERE expires_at < clock_timestamp() - interval '1 minute';
    INSERT INTO interaction_gateway_runtime_transaction_contexts (
        backend_pid, transaction_id, principal_name, principal_generation,
        organization_id, project_id, actor_id, nonce, expires_at, created_at
    ) VALUES (
        pg_backend_pid(), txid_current(), requested_principal_name, requested_principal_generation,
        requested_organization_id, requested_project_id, requested_actor_id, requested_nonce,
        context_expires_at, clock_timestamp()
    );
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_runtime_scope(
    OUT organization_id uuid, OUT project_id uuid, OUT actor_id text
) RETURNS record
LANGUAGE plpgsql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $function$
BEGIN
    SELECT runtime_context.organization_id, runtime_context.project_id, runtime_context.actor_id
      INTO organization_id, project_id, actor_id
      FROM interaction_gateway_runtime_transaction_contexts AS runtime_context
      JOIN interaction_gateway_runtime_principals AS principal
        ON principal.principal_name = runtime_context.principal_name
       AND principal.generation = runtime_context.principal_generation
      JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
     WHERE runtime_context.backend_pid = pg_backend_pid()
       AND runtime_context.transaction_id = txid_current()
       AND runtime_context.principal_name::text = session_user
       AND runtime_context.expires_at > clock_timestamp()
       AND principal.status = 'CURRENT' AND principal.generation = fence.served_generation
       AND pg_has_role(session_user, 'interaction_gateway_runtime', 'member');
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime context is not active' USING ERRCODE = '28000';
    END IF;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_runtime_identity_ready(
    requested_generation bigint, requested_key_id text, requested_key_digest text
) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
    SELECT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_credential_fence AS fence
        JOIN interaction_gateway_runtime_principals AS principal
          ON principal.generation = fence.served_generation
        WHERE fence.singleton AND fence.served_generation = requested_generation
          AND fence.context_key_id = requested_key_id AND fence.context_key_digest = requested_key_digest
          AND principal.principal_name::text = session_user AND principal.status = 'CURRENT'
          AND pg_has_role(session_user, 'interaction_gateway_runtime', 'member')
    );
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_sync_inbound_work_scope()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF NEW.state IN ('PENDING', 'PROCESSING', 'WAITING_SCAN', 'WAITING_CLEANUP') THEN
        INSERT INTO interaction_gateway_inbound_work_scopes(inbound_id, organization_id, project_id, due_at)
        VALUES (NEW.id, NEW.organization_id, NEW.project_id,
            CASE WHEN NEW.state = 'PROCESSING' THEN NEW.processing_expires_at ELSE NEW.next_attempt_at END)
        ON CONFLICT (inbound_id) DO UPDATE SET due_at = EXCLUDED.due_at;
    ELSE
        DELETE FROM interaction_gateway_inbound_work_scopes WHERE inbound_id = NEW.id;
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd
CREATE TRIGGER interaction_gateway_inbound_work_scope_trigger
AFTER INSERT OR UPDATE OF state, next_attempt_at, processing_expires_at ON interaction_gateway_inbound_events
FOR EACH ROW EXECUTE FUNCTION interaction_gateway_sync_inbound_work_scope();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_sync_delivery_work_scope()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    INSERT INTO interaction_gateway_delivery_work_scopes(
        delivery_id, organization_id, project_id, provider_post_id, delivery_active, owner_gate_active, due_at
    ) VALUES (NEW.id, NEW.organization_id, NEW.project_id, NEW.provider_post_id,
        NEW.state IN ('PENDING', 'DELIVERING', 'PROVIDER_ACCEPTED'),
        NEW.owner_gate_id IS NOT NULL AND NEW.owner_gate_decided_at IS NULL AND NEW.state <> 'DEAD_LETTER',
        CASE WHEN NEW.state IN ('DELIVERING', 'PROVIDER_ACCEPTED') AND NEW.lease_expires_at IS NOT NULL
            THEN GREATEST(NEW.next_attempt_at, NEW.lease_expires_at) ELSE NEW.next_attempt_at END)
    ON CONFLICT (delivery_id) DO UPDATE SET provider_post_id = EXCLUDED.provider_post_id,
        delivery_active = EXCLUDED.delivery_active, owner_gate_active = EXCLUDED.owner_gate_active,
        due_at = EXCLUDED.due_at;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd
CREATE TRIGGER interaction_gateway_delivery_work_scope_trigger
AFTER INSERT OR UPDATE OF provider_post_id, next_attempt_at, lease_expires_at, owner_gate_id, owner_gate_decided_at, state
ON interaction_gateway_deliveries
FOR EACH ROW EXECUTE FUNCTION interaction_gateway_sync_delivery_work_scope();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_sync_turn_watch_work_scope()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF NEW.state = 'ACTIVE' THEN
        INSERT INTO interaction_gateway_turn_watch_work_scopes(turn_id, organization_id, project_id, due_at)
        VALUES (NEW.turn_id, NEW.organization_id, NEW.project_id,
            CASE WHEN NEW.lease_expires_at IS NOT NULL THEN GREATEST(NEW.next_poll_at, NEW.lease_expires_at)
                ELSE NEW.next_poll_at END)
        ON CONFLICT (turn_id) DO UPDATE SET due_at = EXCLUDED.due_at;
    ELSE
        DELETE FROM interaction_gateway_turn_watch_work_scopes WHERE turn_id = NEW.turn_id;
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd
CREATE TRIGGER interaction_gateway_turn_watch_work_scope_trigger
AFTER INSERT OR UPDATE OF state, next_poll_at, lease_expires_at ON interaction_gateway_turn_watches
FOR EACH ROW EXECUTE FUNCTION interaction_gateway_sync_turn_watch_work_scope();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_next_work_scope(requested_kind text,
    OUT organization_id uuid, OUT project_id uuid) RETURNS record
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals AS principal
        JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
        WHERE principal.principal_name::text = session_user AND principal.status = 'CURRENT'
          AND principal.generation = fence.served_generation
    ) THEN
        RAISE EXCEPTION 'runtime identity is not active' USING ERRCODE = '28000';
    END IF;
    IF requested_kind = 'INBOUND' THEN
        SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
        FROM interaction_gateway_inbound_work_scopes AS scope
        WHERE scope.due_at <= clock_timestamp() ORDER BY scope.due_at, scope.inbound_id LIMIT 1;
    ELSIF requested_kind = 'DELIVERY' THEN
        SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
        FROM interaction_gateway_delivery_work_scopes AS scope
        WHERE scope.delivery_active AND scope.due_at <= clock_timestamp()
        ORDER BY scope.due_at, scope.delivery_id LIMIT 1;
    ELSIF requested_kind = 'TURN_WATCH' THEN
        SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
        FROM interaction_gateway_turn_watch_work_scopes AS scope
        WHERE scope.due_at <= clock_timestamp() ORDER BY scope.due_at, scope.turn_id LIMIT 1;
    ELSE
        RAISE EXCEPTION 'runtime work kind is invalid' USING ERRCODE = '22023';
    END IF;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_delivery_scope(requested_delivery_id uuid,
    OUT organization_id uuid, OUT project_id uuid) RETURNS record
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals AS principal
        JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
        WHERE principal.principal_name::text = session_user AND principal.status = 'CURRENT'
          AND principal.generation = fence.served_generation
    ) THEN
        RAISE EXCEPTION 'runtime identity is not active' USING ERRCODE = '28000';
    END IF;
    SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
      FROM interaction_gateway_delivery_work_scopes AS scope WHERE scope.delivery_id = requested_delivery_id;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_delivery_scope_by_post(requested_post_id text,
    OUT organization_id uuid, OUT project_id uuid) RETURNS record
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') OR requested_post_id = '' OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals AS principal
        JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
        WHERE principal.principal_name::text = session_user AND principal.status = 'CURRENT'
          AND principal.generation = fence.served_generation
    ) THEN
        RAISE EXCEPTION 'runtime identity is not active' USING ERRCODE = '28000';
    END IF;
    SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
      FROM interaction_gateway_delivery_work_scopes AS scope WHERE scope.provider_post_id = requested_post_id;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_claim_owner_gate_request()
RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
DECLARE requested_key uuid;
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals AS principal
        JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
        WHERE principal.principal_name::text = session_user AND principal.status = 'CURRENT'
          AND principal.generation = fence.served_generation
    ) THEN
        RAISE EXCEPTION 'runtime identity is not active' USING ERRCODE = '28000';
    END IF;
    SELECT idempotency_key INTO requested_key
      FROM interaction_gateway_owner_gate_claim_requests
     WHERE state = 'PENDING' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED;
    IF requested_key IS NULL AND NOT EXISTS (
        SELECT 1 FROM interaction_gateway_delivery_work_scopes WHERE owner_gate_active
    ) THEN
        requested_key := interaction_gateway_security.gen_random_uuid();
        INSERT INTO interaction_gateway_owner_gate_claim_requests(idempotency_key, state)
        VALUES (requested_key, 'PENDING');
    END IF;
    RETURN requested_key;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_bind_owner_gate_request(
    requested_key uuid, requested_gate_id uuid, requested_delivery_id uuid
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
DECLARE active_organization uuid; active_project uuid; delivery_organization uuid; delivery_project uuid;
BEGIN
    SELECT organization_id, project_id INTO active_organization, active_project
      FROM interaction_gateway_runtime_scope();
    SELECT organization_id, project_id INTO delivery_organization, delivery_project
      FROM interaction_gateway_delivery_work_scopes WHERE delivery_id = requested_delivery_id;
    IF (active_organization, active_project) IS DISTINCT FROM (delivery_organization, delivery_project) THEN
        RAISE EXCEPTION 'owner gate delivery scope mismatch' USING ERRCODE = '28000';
    END IF;
    UPDATE interaction_gateway_owner_gate_claim_requests SET
        state = 'CLAIMED', owner_gate_id = requested_gate_id, delivery_id = requested_delivery_id,
        updated_at = clock_timestamp()
    WHERE idempotency_key = requested_key AND state = 'PENDING';
    RETURN FOUND;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_complete_owner_gate_request(requested_key uuid)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
DECLARE bound_delivery uuid; active_organization uuid; active_project uuid; delivery_organization uuid; delivery_project uuid;
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals AS principal
        JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
        WHERE principal.principal_name::text = session_user AND principal.status = 'CURRENT'
          AND principal.generation = fence.served_generation
    ) THEN
        RAISE EXCEPTION 'runtime identity is not active' USING ERRCODE = '28000';
    END IF;
    SELECT delivery_id INTO bound_delivery FROM interaction_gateway_owner_gate_claim_requests
     WHERE idempotency_key = requested_key AND state IN ('PENDING', 'CLAIMED') FOR UPDATE;
    IF NOT FOUND THEN RETURN false; END IF;
    IF bound_delivery IS NOT NULL THEN
        SELECT organization_id, project_id INTO active_organization, active_project FROM interaction_gateway_runtime_scope();
        SELECT organization_id, project_id INTO delivery_organization, delivery_project
          FROM interaction_gateway_delivery_work_scopes WHERE delivery_id = bound_delivery;
        IF (active_organization, active_project) IS DISTINCT FROM (delivery_organization, delivery_project) THEN
            RAISE EXCEPTION 'owner gate completion scope mismatch' USING ERRCODE = '28000';
        END IF;
    END IF;
    UPDATE interaction_gateway_owner_gate_claim_requests SET state = 'COMPLETED', updated_at = clock_timestamp()
     WHERE idempotency_key = requested_key;
    RETURN FOUND;
END
$function$;
-- +goose StatementEnd

-- Триггеры не видят строки, созданные до upgrade, поэтому материализуем
-- безопасные tenant-scoped и не содержащие payload scope-индексы до FORCE RLS.
INSERT INTO interaction_gateway_inbound_work_scopes(inbound_id, organization_id, project_id, due_at)
SELECT id, organization_id, project_id,
    CASE WHEN state = 'PROCESSING' THEN processing_expires_at ELSE next_attempt_at END
FROM interaction_gateway_inbound_events
WHERE state IN ('PENDING', 'PROCESSING', 'WAITING_SCAN', 'WAITING_CLEANUP')
ON CONFLICT (inbound_id) DO UPDATE SET due_at = EXCLUDED.due_at;
INSERT INTO interaction_gateway_delivery_work_scopes(
    delivery_id, organization_id, project_id, provider_post_id, delivery_active, owner_gate_active, due_at
)
SELECT id, organization_id, project_id, provider_post_id,
    state IN ('PENDING', 'DELIVERING', 'PROVIDER_ACCEPTED'),
    owner_gate_id IS NOT NULL AND owner_gate_decided_at IS NULL AND state <> 'DEAD_LETTER',
    CASE WHEN state IN ('DELIVERING', 'PROVIDER_ACCEPTED') AND lease_expires_at IS NOT NULL
        THEN GREATEST(next_attempt_at, lease_expires_at) ELSE next_attempt_at END
FROM interaction_gateway_deliveries
ON CONFLICT (delivery_id) DO UPDATE SET provider_post_id = EXCLUDED.provider_post_id,
    delivery_active = EXCLUDED.delivery_active, owner_gate_active = EXCLUDED.owner_gate_active,
    due_at = EXCLUDED.due_at;
INSERT INTO interaction_gateway_turn_watch_work_scopes(turn_id, organization_id, project_id, due_at)
SELECT turn_id, organization_id, project_id,
    CASE WHEN lease_expires_at IS NOT NULL THEN GREATEST(next_poll_at, lease_expires_at) ELSE next_poll_at END
FROM interaction_gateway_turn_watches WHERE state = 'ACTIVE'
ON CONFLICT (turn_id) DO UPDATE SET due_at = EXCLUDED.due_at;

ALTER TABLE interaction_gateway_inbound_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_inbound_events FORCE ROW LEVEL SECURITY;
-- Старый cursor не содержит tenant/project authority и потому не может быть
-- безопасно перенесён. Gateway восстановит его из проверенного provider path.
DELETE FROM interaction_gateway_cursors;
ALTER TABLE interaction_gateway_cursors ADD COLUMN organization_id uuid NOT NULL;
ALTER TABLE interaction_gateway_cursors ADD COLUMN project_id uuid NOT NULL;
ALTER TABLE interaction_gateway_cursors ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_cursors FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_deliveries FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_upload_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_upload_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_turn_watches ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_turn_watches FORCE ROW LEVEL SECURITY;

CREATE POLICY interaction_gateway_inbound_runtime_scope ON interaction_gateway_inbound_events
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_cursor_runtime_scope ON interaction_gateway_cursors
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_delivery_runtime_scope ON interaction_gateway_deliveries
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_upload_runtime_scope ON interaction_gateway_upload_receipts
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_turn_watch_runtime_scope ON interaction_gateway_turn_watches
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));

REVOKE ALL ON SCHEMA interaction_gateway_security FROM PUBLIC;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA interaction_gateway_security FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_reconcile_runtime_identity(bigint, text, bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_retire_runtime_identity(bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_activate_runtime_context(uuid, uuid, text, name, bigint, text, uuid, bigint, bytea) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_runtime_scope() FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_runtime_identity_ready(bigint, text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_next_work_scope(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_delivery_scope(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_delivery_scope_by_post(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_claim_owner_gate_request() FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_bind_owner_gate_request(uuid, uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_complete_owner_gate_request(uuid) FROM PUBLIC;
REVOKE ALL ON interaction_gateway_runtime_credential_fence, interaction_gateway_runtime_principals,
    interaction_gateway_runtime_context_keys, interaction_gateway_runtime_transaction_contexts,
    interaction_gateway_inbound_work_scopes, interaction_gateway_delivery_work_scopes,
    interaction_gateway_turn_watch_work_scopes, interaction_gateway_owner_gate_claim_requests
    FROM PUBLIC, interaction_gateway_runtime;
GRANT SELECT ON interaction_gateway_runtime_credential_fence TO interaction_gateway_role_controller;
GRANT SELECT, UPDATE ON interaction_gateway_runtime_principals TO interaction_gateway_role_controller;
GRANT USAGE ON SCHEMA interaction_gateway_security TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_security.hmac(bytea, bytea, text) TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_activate_runtime_context(uuid, uuid, text, name, bigint, text, uuid, bigint, bytea)
    TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_runtime_scope() TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_runtime_identity_ready(bigint, text, text) TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_next_work_scope(text) TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_delivery_scope(uuid) TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_delivery_scope_by_post(text) TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_claim_owner_gate_request() TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_bind_owner_gate_request(uuid, uuid, uuid) TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_complete_owner_gate_request(uuid) TO interaction_gateway_runtime;
ALTER FUNCTION interaction_gateway_retire_runtime_identity(bigint) OWNER TO interaction_gateway_role_controller;
GRANT EXECUTE ON FUNCTION interaction_gateway_retire_runtime_identity(bigint) TO interaction_gateway_migrator;

-- +goose Down
-- Forward-only: credential high-watermark, retired identities и durable receipts не откатываются.
SELECT 1;
