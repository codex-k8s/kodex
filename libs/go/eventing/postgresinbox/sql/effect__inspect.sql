-- name: effect__inspect :one
SELECT
    count(*) = 1
    AND bool_and(
        pg_catalog.oidvectortypes(procedure.proargtypes) = 'jsonb'
        AND format_type(procedure.prorettype, NULL) = 'jsonb'
        AND procedure.prokind = 'f'
        AND language.lanname IN ('sql', 'plpgsql')
        AND procedure.provolatile = 'v'
        AND procedure.proparallel = 'u'
        AND NOT procedure.proleakproof
        AND NOT procedure.proretset
        AND NOT procedure.prosecdef
        AND procedure.proconfig IS NULL
        AND NOT pg_catalog.pg_has_role(
            session_user,
            procedure.proowner,
            'MEMBER'
        )
        AND pg_catalog.has_function_privilege(
            session_user::text,
            procedure.oid,
            'EXECUTE'
        )
        AND NOT pg_catalog.has_function_privilege(
            session_user::text,
            procedure.oid,
            'EXECUTE WITH GRANT OPTION'
        )
        AND NOT EXISTS (
            SELECT 1
            FROM pg_catalog.aclexplode(COALESCE(
                procedure.proacl,
                pg_catalog.acldefault('f', procedure.proowner)
            )) AS acl
            WHERE acl.grantee <> procedure.proowner
              AND NOT (
                  acl.grantee = (
                      SELECT oid FROM pg_catalog.pg_roles
                      WHERE rolname = session_user
                  )
                  AND acl.grantor = procedure.proowner
                  AND acl.privilege_type = 'EXECUTE'
                  AND NOT acl.is_grantable
              )
        )
    )
FROM pg_catalog.pg_proc AS procedure
JOIN pg_catalog.pg_namespace AS namespace
  ON namespace.oid = procedure.pronamespace
JOIN pg_catalog.pg_language AS language
  ON language.oid = procedure.prolang
WHERE namespace.nspname = @schema_name
  AND procedure.proname = @function_name
  AND pg_catalog.oidvectortypes(procedure.proargtypes) = 'jsonb';
