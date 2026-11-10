BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object(
 'profile','MANAGED_WORKSPACE_V1',
 'database',current_database(), 'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'managedWorkspaceColumnReady',EXISTS (
  SELECT 1 FROM pg_attribute attribute
  JOIN pg_attrdef defaults ON defaults.adrelid=attribute.attrelid AND defaults.adnum=attribute.attnum
  WHERE attribute.attrelid='control_plane.runtime_environment_versions'::regclass
    AND attribute.attname='managed_workspace' AND NOT attribute.attisdropped
    AND attribute.atttypid='boolean'::regtype AND attribute.attnotnull
    AND pg_get_expr(defaults.adbin,defaults.adrelid)='false')
)::text;
ROLLBACK;
