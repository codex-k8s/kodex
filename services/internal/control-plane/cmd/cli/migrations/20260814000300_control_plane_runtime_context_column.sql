-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;

-- PostgreSQL разрешает конфликт имён PL/pgSQL variable/column только при
-- первом выполнении statement. Квалифицированные ссылки сохраняют backend-local
-- cleanup и устраняют runtime ambiguity без изменения security boundary.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.activate_runtime_context(
    requested_organization_id uuid,
    requested_project_id uuid,
    requested_actor_id uuid,
    requested_principal_name name,
    requested_principal_generation bigint,
    requested_key_id text,
    requested_nonce uuid,
    requested_expires_unix_micro bigint,
    requested_signature bytea
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE
    active_secret bytea;
    canonical text;
    context_expires_at timestamptz;
BEGIN
    context_expires_at := to_timestamp(requested_expires_unix_micro::numeric / 1000000);
    IF requested_principal_name::text <> session_user
       OR requested_expires_unix_micro <= floor(extract(epoch FROM clock_timestamp()) * 1000000)
       OR requested_expires_unix_micro > floor(extract(epoch FROM clock_timestamp() + interval '10 seconds') * 1000000)
       OR NOT pg_has_role(session_user, 'control_plane_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime context identity is invalid' USING ERRCODE = '28000';
    END IF;

    SELECT context_key.secret
      INTO active_secret
      FROM control_plane.runtime_context_keys AS context_key
     WHERE context_key.key_id = requested_key_id
       AND context_key.status = 'ACTIVE'
     FOR SHARE;
    IF active_secret IS NULL THEN
        RAISE EXCEPTION 'runtime context key is unavailable' USING ERRCODE = '28000';
    END IF;

    PERFORM 1
      FROM control_plane.runtime_principals AS principal
      JOIN pg_catalog.pg_roles AS role
        ON role.rolname = principal.principal_name
     WHERE principal.principal_name = requested_principal_name
       AND principal.generation = requested_principal_generation
       AND principal.status IN ('CURRENT', 'NEXT', 'PREVIOUS')
       AND clock_timestamp() >= principal.not_before
       AND clock_timestamp() < principal.not_after
       AND role.rolcanlogin
       AND NOT role.rolsuper
       AND NOT role.rolbypassrls
       AND pg_has_role(role.rolname, 'control_plane_runtime', 'member')
     FOR SHARE OF principal;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime principal is not active' USING ERRCODE = '28000';
    END IF;

    canonical := 'v1' || chr(10)
        || requested_principal_name::text || chr(10)
        || requested_principal_generation::text || chr(10)
        || requested_organization_id::text || chr(10)
        || coalesce(requested_project_id::text, '') || chr(10)
        || requested_actor_id::text || chr(10)
        || requested_nonce::text || chr(10)
        || requested_expires_unix_micro::text;
    IF control_plane_extensions.hmac(
        convert_to(canonical, 'UTF8'), active_secret, 'sha256'
    )
       <> requested_signature THEN
        RAISE EXCEPTION 'runtime context signature is invalid' USING ERRCODE = '28000';
    END IF;

    DELETE FROM control_plane.runtime_transaction_contexts AS runtime_context
     WHERE runtime_context.backend_pid = pg_backend_pid()
       AND runtime_context.transaction_id <> txid_current()
       AND runtime_context.expires_at < clock_timestamp() - interval '1 minute';

    INSERT INTO control_plane.runtime_transaction_contexts (
        backend_pid,
        transaction_id,
        principal_name,
        principal_generation,
        organization_id,
        project_id,
        actor_id,
        nonce,
        expires_at,
        created_at
    ) VALUES (
        pg_backend_pid(),
        txid_current(),
        requested_principal_name,
        requested_principal_generation,
        requested_organization_id,
        requested_project_id,
        requested_actor_id,
        requested_nonce,
        context_expires_at,
        clock_timestamp()
    );
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE
    stored_version bigint;
BEGIN
    SELECT version
      INTO stored_version
      FROM control_plane.schema_state
     WHERE singleton = true
     FOR UPDATE;

    IF stored_version <> 20260814000200 THEN
        RAISE EXCEPTION 'control-plane runtime context column source version is invalid'
            USING ERRCODE = '55000';
    END IF;

    UPDATE control_plane.schema_state
       SET version = 20260814000300,
           migrated_at = clock_timestamp()
     WHERE singleton = true;
END;
$$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- Forward-only: неоднозначное тело функции не восстанавливается.
SELECT 1;
