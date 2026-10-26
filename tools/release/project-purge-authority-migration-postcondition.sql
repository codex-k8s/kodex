-- name: project_purge_authority_migration_postcondition :one
-- Только права и schema metadata; имена LOGIN, credentials и строки контекста не выдаются.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '500ms';
WITH RECURSIVE runtime_members(oid) AS (
    SELECT oid FROM pg_roles WHERE rolname='control_plane_runtime'
    UNION
    SELECT membership.member FROM pg_auth_members membership JOIN runtime_members parent ON parent.oid=membership.roleid
), members AS (
    SELECT role.oid,role.rolcanlogin,role.rolsuper,role.rolcreaterole,role.rolcreatedb,role.rolbypassrls
    FROM pg_roles role JOIN runtime_members USING (oid)
), reachable AS (
    -- INHERIT FALSE не запрещает SET ROLE; проверяется и эта цепочка полномочий.
    SELECT role.oid,role.rolsuper,role.rolcreaterole,role.rolcreatedb,role.rolbypassrls FROM pg_roles role
    WHERE EXISTS (SELECT 1 FROM members member WHERE pg_has_role(member.oid,role.oid,'SET'))
), targets AS (
    SELECT oid,relowner,relacl FROM pg_class
    WHERE oid IN ('control_plane.project_purge_context'::regclass,'control_plane.project_purge_targets'::regclass)
), authorizer AS (
    SELECT oid,proowner,prosecdef,provolatile,proconfig,prorettype,prolang,prosrc,proacl
    FROM pg_proc WHERE oid=to_regprocedure('control_plane.project_purge_authorized()')
)
SELECT jsonb_build_object(
    'readOnly',current_setting('transaction_read_only'),
    'database',current_database(),
    'gooseVersion',(SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version),
    'tables',(SELECT count(*) FROM targets),
    'runtimeRoles',(SELECT count(*) FROM members),
    'runtimeLogins',(SELECT count(*) FROM members WHERE rolcanlogin),
    'tableOwners',(SELECT bool_and(pg_get_userbyid(relowner)='control_plane_owner') FROM targets),
    'runtimeRolesUnprivileged',NOT EXISTS (SELECT 1 FROM reachable WHERE rolsuper OR rolcreaterole OR rolcreatedb OR rolbypassrls
        OR pg_has_role(oid,'control_plane_owner','MEMBER')),
    'runtimeRead',(SELECT bool_and(has_table_privilege(member.oid,target.oid,'SELECT')) FROM members member CROSS JOIN targets target),
    'runtimeMutationDenied',NOT EXISTS (
        SELECT 1 FROM reachable member CROSS JOIN targets target
        CROSS JOIN unnest(ARRAY['INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER','MAINTAIN']) privilege
        WHERE has_table_privilege(member.oid,target.oid,privilege)),
    'runtimeColumnMutationDenied',NOT EXISTS (
        SELECT 1 FROM reachable member CROSS JOIN targets target
        CROSS JOIN unnest(ARRAY['INSERT','UPDATE','REFERENCES']) privilege
        WHERE has_any_column_privilege(member.oid,target.oid,privilege)),
    'runtimeGrantOptionDenied',NOT EXISTS (SELECT 1 FROM reachable member CROSS JOIN targets target
        WHERE has_table_privilege(member.oid,target.oid,'SELECT WITH GRANT OPTION')),
    'publicPrivilegesRevoked',NOT EXISTS (
        SELECT 1 FROM targets target CROSS JOIN LATERAL aclexplode(coalesce(target.relacl,acldefault('r',target.relowner))) acl
        WHERE acl.grantee=0
        UNION ALL
        SELECT 1 FROM pg_attribute column_row JOIN targets target ON target.oid=column_row.attrelid
        CROSS JOIN LATERAL aclexplode(nullif(column_row.attacl,ARRAY[]::aclitem[])) acl WHERE acl.grantee=0),
    'ownerWrite',(SELECT bool_and(has_table_privilege('control_plane_owner',target.oid,privilege)) FROM targets target
        CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE']) privilege),
    'authorizationFunction',EXISTS (
        SELECT 1 FROM authorizer function_row WHERE function_row.prosecdef AND function_row.provolatile='s'
            AND pg_get_userbyid(function_row.proowner)='control_plane_owner'
            AND function_row.proconfig=ARRAY['search_path=pg_catalog']
            AND function_row.prorettype='boolean'::regtype
            AND function_row.prolang=(SELECT oid FROM pg_language WHERE lanname='sql')
            AND function_row.prosrc=$body$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.project_purge_context context
        WHERE context.transaction_id = pg_current_xact_id_if_assigned()
          AND context.backend_pid = pg_backend_pid()
    );
$body$
            AND has_function_privilege('control_plane_runtime',function_row.oid,'EXECUTE')
            AND NOT EXISTS (SELECT 1 FROM aclexplode(coalesce(function_row.proacl,acldefault('f',function_row.proowner))) acl
                WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')),
    'contextsEmpty',NOT EXISTS (SELECT 1 FROM control_plane.project_purge_context)
        AND NOT EXISTS (SELECT 1 FROM control_plane.project_purge_targets)
)::text;
ROLLBACK;
