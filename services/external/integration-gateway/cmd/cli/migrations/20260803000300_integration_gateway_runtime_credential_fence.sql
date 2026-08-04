-- +goose Up
CREATE TABLE integration_gateway.runtime_credential_fence (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    current_high_watermark bigint NOT NULL CHECK (current_high_watermark >= 0),
    served_readback_generation bigint NOT NULL CHECK (
        served_readback_generation >= 0
        AND served_readback_generation <= current_high_watermark
    ),
    updated_at timestamptz NOT NULL
);
INSERT INTO integration_gateway.runtime_credential_fence (
    singleton, current_high_watermark, served_readback_generation, updated_at
) VALUES (true, 0, 0, clock_timestamp());
UPDATE integration_gateway.runtime_credential_fence SET
    current_high_watermark = coalesce((
        SELECT max(generation) FROM integration_gateway.runtime_principals WHERE status = 'CURRENT'
    ), 0),
    updated_at = clock_timestamp()
WHERE singleton;

REVOKE INSERT, UPDATE, DELETE ON integration_gateway.runtime_principals
    FROM integration_gateway_migrator;
REVOKE INSERT, UPDATE, DELETE ON integration_gateway.runtime_context_keys
    FROM integration_gateway_migrator;
REVOKE UPDATE, DELETE ON integration_gateway.runtime_credential_fence
    FROM integration_gateway_migrator;
GRANT SELECT ON integration_gateway.runtime_credential_fence
    TO integration_gateway_migrator;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.runtime_principals
    TO integration_gateway_role_controller;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.runtime_context_keys
    TO integration_gateway_role_controller;
GRANT SELECT, UPDATE ON integration_gateway.runtime_credential_fence
    TO integration_gateway_role_controller;

-- Старый migration image не может обойти owner procedure через прежние entrypoints.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.bootstrap_runtime_principal(text, bigint, text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    RAISE EXCEPTION 'direct runtime principal bootstrap is retired' USING ERRCODE = '0A000';
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.retire_runtime_principal(text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    RAISE EXCEPTION 'direct runtime principal retirement is retired' USING ERRCODE = '0A000';
END
$function$;
-- +goose StatementEnd

-- Единственный owner-side prepare path. Promotion gN→gN+1 разрешена только
-- если gN+1 уже была durable NEXT; served high-watermark меняет отдельный
-- exact readback step.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.reconcile_runtime_credentials(
    requested_principals jsonb,
    requested_context_key_id text,
    requested_context_key bytea,
    requested_current_generation bigint,
    requested_served_generation bigint
) RETURNS TABLE(reconciled_principals bigint, reconciled_keys bigint, reconciled_fences bigint)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
DECLARE
    fence integration_gateway.runtime_credential_fence%ROWTYPE;
    candidate record;
    current_count integer;
    desired_count integer;
    old_status text;
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_migrator', 'member')
       OR current_user <> 'integration_gateway_role_controller'
       OR jsonb_typeof(requested_principals) <> 'array'
       OR jsonb_array_length(requested_principals) NOT BETWEEN 1 AND 3
       OR requested_current_generation <= 0
       OR requested_served_generation < 0
       OR requested_served_generation > requested_current_generation
       OR requested_context_key_id = '' OR octet_length(requested_context_key) < 32 THEN
        RAISE EXCEPTION 'runtime credential reconcile input is invalid' USING ERRCODE = '28000';
    END IF;

    SELECT * INTO STRICT fence
      FROM integration_gateway.runtime_credential_fence
     WHERE singleton FOR UPDATE;
    IF requested_current_generation < fence.current_high_watermark
       OR requested_current_generation > fence.current_high_watermark + 1
       OR requested_served_generation <> fence.served_readback_generation THEN
        RAISE EXCEPTION 'runtime credential rollback or skip is forbidden' USING ERRCODE = '28000';
    END IF;

    SELECT count(*), count(*) FILTER (WHERE status = 'CURRENT')
      INTO desired_count, current_count
      FROM jsonb_to_recordset(requested_principals) AS desired(
          principal_name text, generation bigint, status text,
          not_before timestamptz, not_after timestamptz, password text
      );
    IF desired_count <> jsonb_array_length(requested_principals) OR current_count <> 1 THEN
        RAISE EXCEPTION 'runtime credential desired set is invalid' USING ERRCODE = '28000';
    END IF;

    FOR candidate IN
        SELECT * FROM jsonb_to_recordset(requested_principals) AS desired(
            principal_name text, generation bigint, status text,
            not_before timestamptz, not_after timestamptz, password text
        ) ORDER BY generation
    LOOP
        IF candidate.principal_name <> ('integration_gateway_runtime_g' || candidate.generation::text)
           OR candidate.generation <= 0
           OR candidate.status NOT IN ('CURRENT', 'NEXT', 'PREVIOUS')
           OR candidate.not_before IS NULL OR candidate.not_after <= candidate.not_before
           OR length(candidate.password) NOT BETWEEN 32 AND 1024
           OR candidate.password <> btrim(candidate.password)
           OR (candidate.status = 'CURRENT' AND candidate.generation <> requested_current_generation)
           OR (candidate.status = 'NEXT' AND candidate.generation <> requested_current_generation + 1)
           OR (candidate.status = 'PREVIOUS' AND candidate.generation <> requested_current_generation - 1) THEN
            RAISE EXCEPTION 'runtime credential desired principal is invalid' USING ERRCODE = '28000';
        END IF;
        SELECT status INTO old_status
          FROM integration_gateway.runtime_principals
         WHERE principal_name::text = candidate.principal_name FOR UPDATE;
        IF FOUND AND NOT (
            old_status = candidate.status
            OR old_status = 'NEXT' AND candidate.status = 'CURRENT'
            OR old_status = 'CURRENT' AND candidate.status = 'PREVIOUS'
        ) THEN
            RAISE EXCEPTION 'runtime credential state transition is forbidden' USING ERRCODE = '28000';
        END IF;
        IF candidate.status = 'CURRENT' AND requested_current_generation = fence.current_high_watermark + 1
           AND coalesce(old_status, '') <> 'NEXT' THEN
            RAISE EXCEPTION 'runtime credential promotion requires durable NEXT' USING ERRCODE = '28000';
        END IF;

        IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = candidate.principal_name) THEN
            EXECUTE format(
                'ALTER ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS',
                candidate.principal_name, candidate.password
            );
        ELSE
            EXECUTE format(
                'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS',
                candidate.principal_name, candidate.password
            );
        END IF;
        EXECUTE format('GRANT integration_gateway_runtime TO %I', candidate.principal_name);
        INSERT INTO integration_gateway.runtime_principals (
            principal_name, generation, status, not_before, not_after, updated_at
        ) VALUES (
            candidate.principal_name::name, candidate.generation, candidate.status,
            candidate.not_before, candidate.not_after, clock_timestamp()
        ) ON CONFLICT (principal_name) DO UPDATE SET
            status = EXCLUDED.status, not_before = EXCLUDED.not_before,
            not_after = EXCLUDED.not_after, updated_at = EXCLUDED.updated_at;
    END LOOP;

    FOR candidate IN
        SELECT principal_name::text AS principal_name
          FROM integration_gateway.runtime_principals
         WHERE principal_name::text NOT IN (
             SELECT principal_name FROM jsonb_to_recordset(requested_principals)
                AS desired(principal_name text)
         ) AND status <> 'RETIRED'
         FOR UPDATE
    LOOP
        UPDATE integration_gateway.runtime_principals SET
            status = 'RETIRED', updated_at = clock_timestamp()
         WHERE principal_name::text = candidate.principal_name;
        PERFORM pg_catalog.pg_terminate_backend(pid)
          FROM pg_catalog.pg_stat_activity
         WHERE usename = candidate.principal_name AND pid <> pg_backend_pid();
        EXECUTE format('REVOKE integration_gateway_runtime FROM %I', candidate.principal_name);
        EXECUTE format('ALTER ROLE %I NOLOGIN', candidate.principal_name);
    END LOOP;

    UPDATE integration_gateway.runtime_context_keys SET
        status = 'RETIRED', updated_at = clock_timestamp()
     WHERE key_id <> requested_context_key_id AND status = 'ACTIVE';
    INSERT INTO integration_gateway.runtime_context_keys (key_id, secret, status, updated_at)
    VALUES (requested_context_key_id, requested_context_key, 'ACTIVE', clock_timestamp())
    ON CONFLICT (key_id) DO UPDATE SET updated_at = EXCLUDED.updated_at
      WHERE integration_gateway.runtime_context_keys.status = 'ACTIVE'
        AND integration_gateway.runtime_context_keys.secret = EXCLUDED.secret;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime context key rollback is forbidden' USING ERRCODE = '28000';
    END IF;

    UPDATE integration_gateway.runtime_credential_fence SET
        current_high_watermark = requested_current_generation,
        updated_at = clock_timestamp()
     WHERE singleton;
    RETURN QUERY SELECT desired_count::bigint, 1::bigint, 1::bigint;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.confirm_runtime_credential_served(
    requested_generation bigint,
    readback_session_user text
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
DECLARE
    fence integration_gateway.runtime_credential_fence%ROWTYPE;
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_migrator', 'member')
       OR current_user <> 'integration_gateway_role_controller'
       OR readback_session_user <> ('integration_gateway_runtime_g' || requested_generation::text) THEN
        RAISE EXCEPTION 'runtime credential served readback is invalid' USING ERRCODE = '28000';
    END IF;
    SELECT * INTO STRICT fence FROM integration_gateway.runtime_credential_fence
     WHERE singleton FOR UPDATE;
    IF requested_generation <> fence.current_high_watermark
       OR requested_generation < fence.served_readback_generation
       OR NOT EXISTS (
           SELECT 1 FROM integration_gateway.runtime_principals
            WHERE generation = requested_generation AND status = 'CURRENT'
       ) THEN
        RAISE EXCEPTION 'runtime credential served generation is stale' USING ERRCODE = '28000';
    END IF;
    UPDATE integration_gateway.runtime_credential_fence SET
        served_readback_generation = requested_generation, updated_at = clock_timestamp()
     WHERE singleton;
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
        SELECT 1
          FROM integration_gateway.runtime_principals AS principal
          JOIN integration_gateway.runtime_credential_fence AS fence ON fence.singleton
         WHERE principal.principal_name::text = session_user
           AND fence.served_readback_generation = fence.current_high_watermark
           AND (
               principal.status = 'CURRENT' AND principal.generation = fence.current_high_watermark
               OR principal.status = 'PREVIOUS' AND principal.generation + 1 = fence.current_high_watermark
           )
           AND clock_timestamp() >= principal.not_before
           AND clock_timestamp() < principal.not_after
    );
END
$function$;
-- +goose StatementEnd

GRANT EXECUTE ON FUNCTION integration_gateway.reconcile_runtime_credentials(jsonb, text, bytea, bigint, bigint)
    TO integration_gateway_migrator;
GRANT EXECUTE ON FUNCTION integration_gateway.confirm_runtime_credential_served(bigint, text)
    TO integration_gateway_migrator;
ALTER FUNCTION integration_gateway.reconcile_runtime_credentials(jsonb, text, bytea, bigint, bigint)
    OWNER TO integration_gateway_role_controller;
ALTER FUNCTION integration_gateway.confirm_runtime_credential_served(bigint, text)
    OWNER TO integration_gateway_role_controller;
ALTER FUNCTION integration_gateway.runtime_identity_ready()
    OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.runtime_credential_fence
    OWNER TO integration_gateway_owner;

-- +goose Down
-- Forward-only: credential high-watermark нельзя удалять или уменьшать.
SELECT 1;
