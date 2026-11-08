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
DO $successor_backup$
DECLARE target text;
BEGIN
 FOREACH target IN ARRAY ARRAY['oidc_identity_observations','oidc_identity_policies',
   'oidc_identity_policy_current','oidc_identity_sessions','integration_connections',
   'integration_invocations','integration_personal_credential_revisions','integration_personal_credential_tests',
   'integration_personal_credentials','integration_personal_invocation_admissions',
   'integration_personal_invocation_credentials','runs','authority_proof_revision_counter'] LOOP
  IF to_regclass('control_plane.'||target) IS NOT NULL THEN
   EXECUTE format('GRANT SELECT ON control_plane.%I TO kodex_backup_reader',target);
  END IF;
 END LOOP;
END $successor_backup$;
COMMIT;
