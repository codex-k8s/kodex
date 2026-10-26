-- name: project_purge_authority_drift_admin_role :exec
-- ADMIN OPTION позволяет выдать себе SET даже при исходных SET FALSE и INHERIT FALSE.
CREATE ROLE purge_authority_probe_writer NOLOGIN;
GRANT UPDATE ON control_plane.project_purge_context TO purge_authority_probe_writer;
DO $probe$
DECLARE runtime_login name;
BEGIN
    SELECT rolname INTO STRICT runtime_login FROM pg_roles
    WHERE rolcanlogin AND NOT rolsuper AND pg_has_role(oid,'control_plane_runtime','MEMBER');
    EXECUTE format('GRANT purge_authority_probe_writer TO %I WITH ADMIN TRUE, INHERIT FALSE, SET FALSE',runtime_login);
    IF has_table_privilege(runtime_login,'control_plane.project_purge_context','UPDATE')
        OR pg_has_role(runtime_login,'purge_authority_probe_writer','SET')
        OR NOT EXISTS (SELECT 1 FROM pg_auth_members membership JOIN pg_roles member ON member.oid=membership.member
            JOIN pg_roles target ON target.oid=membership.roleid
            WHERE member.rolname=runtime_login AND target.rolname='purge_authority_probe_writer' AND membership.admin_option) THEN
        RAISE EXCEPTION 'invalid isolated ADMIN OPTION privilege fixture';
    END IF;
END $probe$;
