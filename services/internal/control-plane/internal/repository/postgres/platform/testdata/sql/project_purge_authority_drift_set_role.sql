-- name: project_purge_authority_drift_set_role :exec
-- Только одноразовый PostgreSQL: SET даёт запись без автоматически наследуемых прав.
CREATE ROLE purge_authority_probe_writer NOLOGIN;
GRANT UPDATE ON control_plane.project_purge_context TO purge_authority_probe_writer;
DO $probe$
DECLARE runtime_login name;
BEGIN
    SELECT rolname INTO STRICT runtime_login FROM pg_roles
    WHERE rolcanlogin AND NOT rolsuper AND pg_has_role(oid,'control_plane_runtime','MEMBER');
    EXECUTE format('GRANT purge_authority_probe_writer TO %I WITH INHERIT FALSE, SET TRUE',runtime_login);
    IF has_table_privilege(runtime_login,'control_plane.project_purge_context','UPDATE')
        OR NOT pg_has_role(runtime_login,'purge_authority_probe_writer','SET') THEN
        RAISE EXCEPTION 'invalid isolated SET ROLE privilege fixture';
    END IF;
END $probe$;
