-- name: integration_github_backup_fixture :exec
-- Только уже проверенная disposable БД и её отдельный PostgreSQL-контейнер.
BEGIN;
DO $fixture$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='kodex_backup_reader') THEN
  CREATE ROLE kodex_backup_reader LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION BYPASSRLS;
 END IF;
END $fixture$;
SET LOCAL ROLE control_plane_owner;
ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader;
GRANT SELECT ON control_plane.session_workspace_storage,control_plane.session_archives,
 control_plane.session_archive_tasks,control_plane.session_workspace_volumes,
 control_plane.session_workspace_volume_tasks,control_plane.session_workspace_volume_attempts TO kodex_backup_reader;
COMMIT;
