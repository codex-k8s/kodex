-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
-- Foundation bootstrap заранее выдаёт role controller право администрировать
-- текущее runtime-поколение. Повторная reconciliation не должна пытаться
-- выдать эту же роль обратно собственному grantor.
GRANT CREATE ON SCHEMA control_plane TO control_plane_role_controller;
RESET ROLE;
SET ROLE control_plane_role_controller;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.bootstrap_runtime_principal(
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
    IF NOT EXISTS (
        SELECT 1
          FROM pg_catalog.pg_auth_members AS membership
          JOIN pg_catalog.pg_roles AS granted_role
            ON granted_role.oid = membership.roleid
          JOIN pg_catalog.pg_roles AS member_role
            ON member_role.oid = membership.member
         WHERE granted_role.rolname = requested_name
           AND member_role.rolname = 'control_plane_role_controller'
           AND membership.admin_option
    ) THEN
        EXECUTE format(
            'GRANT %I TO control_plane_role_controller WITH ADMIN OPTION',
            requested_name
        );
    END IF;
    IF NOT pg_has_role(requested_name, 'control_plane_runtime', 'member')
       OR NOT pg_has_role(
            'control_plane_role_controller', requested_name, 'member'
       ) THEN
        RAISE EXCEPTION 'runtime principal bootstrap readback failed'
            USING ERRCODE = '55000';
    END IF;
END
$function$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.bootstrap_runtime_principal(
    text, bigint, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.bootstrap_runtime_principal(
    text, bigint, text
) TO control_plane_owner;

RESET ROLE;
SET ROLE control_plane_owner;
REVOKE CREATE ON SCHEMA control_plane FROM control_plane_role_controller;
RESET ROLE;

-- +goose Down
SELECT 1;
