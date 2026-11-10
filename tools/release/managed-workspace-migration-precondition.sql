-- Добавляется только opt-in колонка версии окружения; без dump, ACL и изменения старых строк.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object(
 'profile','MANAGED_WORKSPACE_PREDECESSOR_V1',
 'database',current_database(), 'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'environmentVersionsReady',to_regclass('control_plane.runtime_environment_versions') IS NOT NULL,
 'managedWorkspaceAbsent',NOT EXISTS (
  SELECT 1 FROM pg_attribute
  WHERE attrelid=to_regclass('control_plane.runtime_environment_versions')
    AND attname='managed_workspace' AND NOT attisdropped)
)::text;
ROLLBACK;
