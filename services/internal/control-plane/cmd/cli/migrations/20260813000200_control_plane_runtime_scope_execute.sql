-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;

REVOKE ALL ON FUNCTION control_plane.runtime_scope() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.runtime_scope() TO control_plane_runtime;

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

    IF stored_version <> 20260813000100 THEN
        RAISE EXCEPTION 'control-plane runtime scope grant source version is invalid'
            USING ERRCODE = '55000';
    END IF;
    IF EXISTS (
        SELECT 1
          FROM pg_proc AS p
          JOIN pg_namespace AS n
            ON n.oid = p.pronamespace
          CROSS JOIN LATERAL aclexplode(
              COALESCE(p.proacl, acldefault('f', p.proowner))
          ) AS acl
         WHERE n.nspname = 'control_plane'
           AND p.proname = 'runtime_scope'
           AND pg_get_function_identity_arguments(p.oid) = ''
           AND acl.grantee = 0
           AND acl.privilege_type = 'EXECUTE'
    ) OR NOT has_function_privilege(
           'control_plane_runtime',
           'control_plane.runtime_scope()',
           'EXECUTE'
       ) THEN
        RAISE EXCEPTION 'control-plane runtime scope grant is invalid'
            USING ERRCODE = '42501';
    END IF;

    UPDATE control_plane.schema_state
       SET version = 20260813000200,
           migrated_at = clock_timestamp()
     WHERE singleton = true;
END;
$$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- Forward-only: runtime RLS policies require this exact function grant.
SELECT 1;
