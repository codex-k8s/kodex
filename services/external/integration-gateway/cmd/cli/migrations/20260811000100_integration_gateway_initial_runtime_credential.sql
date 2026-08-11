-- +goose Up
RESET ROLE;
SET ROLE integration_gateway_owner;
GRANT CREATE ON SCHEMA integration_gateway TO integration_gateway_role_controller;
RESET ROLE;
SET ROLE integration_gateway_role_controller;

-- Первое поколение проходит тот же durable NEXT -> CURRENT протокол, что и
-- последующие ротации. Эта функция разрешена только на пустом genesis fence.
-- +goose StatementBegin
CREATE FUNCTION integration_gateway.stage_initial_runtime_credential(
    requested_principal_name text,
    requested_generation bigint,
    requested_password text,
    requested_not_before timestamptz,
    requested_not_after timestamptz
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
DECLARE
    fence integration_gateway.runtime_credential_fence%ROWTYPE;
    role_safe boolean;
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_migrator', 'member')
       OR current_user <> 'integration_gateway_role_controller'
       OR requested_generation <> 1
       OR requested_principal_name <> 'integration_gateway_runtime_g1'
       OR length(requested_password) NOT BETWEEN 32 AND 1024
       OR requested_password <> btrim(requested_password)
       OR requested_not_before IS NULL
       OR requested_not_after <= requested_not_before THEN
        RAISE EXCEPTION 'initial runtime credential input is invalid'
            USING ERRCODE = '28000';
    END IF;

    SELECT * INTO STRICT fence
      FROM integration_gateway.runtime_credential_fence
     WHERE singleton
     FOR UPDATE;
    IF fence.current_high_watermark <> 0
       OR fence.served_readback_generation <> 0
       OR EXISTS (SELECT 1 FROM integration_gateway.runtime_principals)
       OR EXISTS (SELECT 1 FROM integration_gateway.runtime_context_keys) THEN
        RAISE EXCEPTION 'initial runtime credential state is not empty'
            USING ERRCODE = '55000';
    END IF;

    SELECT role.rolcanlogin
           AND NOT role.rolsuper
           AND NOT role.rolcreatedb
           AND NOT role.rolcreaterole
           AND NOT role.rolreplication
           AND NOT role.rolbypassrls
      INTO role_safe
      FROM pg_catalog.pg_roles AS role
     WHERE role.rolname = requested_principal_name;
    IF role_safe IS NOT NULL AND NOT role_safe THEN
        RAISE EXCEPTION 'existing initial runtime principal role is unsafe'
            USING ERRCODE = '42501';
    END IF;
    IF role_safe IS NULL THEN
        EXECUTE format(
            'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS',
            requested_principal_name,
            requested_password
        );
    ELSE
        EXECUTE format(
            'ALTER ROLE %I LOGIN PASSWORD %L NOCREATEROLE INHERIT',
            requested_principal_name,
            requested_password
        );
    END IF;
    EXECUTE format(
        'GRANT integration_gateway_runtime TO %I',
        requested_principal_name
    );
    INSERT INTO integration_gateway.runtime_principals (
        principal_name,
        generation,
        status,
        not_before,
        not_after,
        updated_at
    ) VALUES (
        requested_principal_name::name,
        requested_generation,
        'NEXT',
        requested_not_before,
        requested_not_after,
        clock_timestamp()
    );
END
$function$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION integration_gateway.stage_initial_runtime_credential(
    text, bigint, text, timestamptz, timestamptz
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION integration_gateway.stage_initial_runtime_credential(
    text, bigint, text, timestamptz, timestamptz
) TO integration_gateway_migrator;

RESET ROLE;
SET ROLE integration_gateway_owner;
REVOKE CREATE ON SCHEMA integration_gateway FROM integration_gateway_role_controller;
RESET ROLE;

-- +goose Down
-- Forward-only: an initialized credential generation is never removed.
SELECT 1;
