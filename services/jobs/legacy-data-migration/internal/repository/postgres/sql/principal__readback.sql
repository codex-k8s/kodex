-- name: principal__readback :one
WITH RECURSIVE session_role AS (
    SELECT oid
    FROM pg_catalog.pg_roles
    WHERE rolname = session_user
), required_role AS (
    SELECT role.*
    FROM pg_catalog.pg_roles AS role
    WHERE role.rolname = @required_role
), memberships(roleid) AS (
    SELECT member.roleid
    FROM pg_catalog.pg_auth_members AS member
    JOIN session_role ON session_role.oid = member.member
    UNION
    SELECT parent.roleid
    FROM pg_catalog.pg_auth_members AS parent
    JOIN memberships ON memberships.roleid = parent.member
)
SELECT current_user = session_user,
       role.rolcanlogin
       AND NOT role.rolsuper
       AND NOT role.rolcreatedb
       AND NOT role.rolcreaterole
       AND NOT role.rolreplication
       AND NOT role.rolbypassrls
       AND NOT EXISTS (
           SELECT 1
           FROM memberships
           JOIN pg_catalog.pg_roles AS inherited ON inherited.oid = memberships.roleid
           WHERE @required_role = '' OR inherited.rolname <> @required_role
       )
       AND (
           @required_role = ''
           OR NOT EXISTS (
               SELECT 1
               FROM information_schema.tables AS candidate
               WHERE candidate.table_schema NOT IN ('pg_catalog', 'information_schema')
                 AND candidate.table_name NOT IN (
                     'matter_codex_legacy_data_cutovers', 'legacy_data_cutovers'
                 )
                 AND (
                     has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'INSERT')
                     OR has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'UPDATE')
                     OR has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'DELETE')
                     OR has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'TRUNCATE')
                     OR has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'REFERENCES')
                     OR has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'TRIGGER')
               )
           )
       )
       AND (
           @required_role = ''
           OR NOT EXISTS (
               SELECT 1
               FROM pg_catalog.pg_namespace AS namespace
               WHERE namespace.nspname <> 'pg_catalog'
                 AND namespace.nspname <> 'information_schema'
                 AND namespace.nspname !~ '^pg_toast'
                 AND namespace.nspname !~ '^pg_temp_'
                 AND has_schema_privilege(session_user, namespace.oid, 'CREATE')
           )
       ),
       CASE WHEN @required_role = '' THEN true
            ELSE EXISTS (
                     SELECT 1
                     FROM required_role
                     WHERE NOT required_role.rolcanlogin
                       AND NOT required_role.rolsuper
                       AND NOT required_role.rolcreatedb
                       AND NOT required_role.rolcreaterole
                       AND NOT required_role.rolreplication
                       AND NOT required_role.rolbypassrls
                 )
                 AND pg_has_role(session_user, @required_role, 'usage')
                 AND (SELECT count(*) FROM memberships) = 1
       END
FROM pg_catalog.pg_roles AS role
WHERE role.rolname = session_user;
