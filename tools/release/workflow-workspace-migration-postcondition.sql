BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object(
 'profile','WORKFLOW_WORKSPACE_V1',
 'database',current_database(), 'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'workflowVersionColumnReady',EXISTS (
  SELECT 1 FROM pg_attribute attribute
  WHERE attribute.attrelid='control_plane.session_workspace_storage'::regclass
    AND attribute.attname='workflow_version_id' AND NOT attribute.attisdropped
    AND attribute.atttypid='uuid'::regtype AND NOT attribute.attnotnull
 ),
 'rootVersionReferenceReady',EXISTS (
  SELECT 1 FROM pg_constraint reference
  JOIN pg_attribute attribute ON attribute.attrelid=reference.conrelid
    AND attribute.attname='workflow_version_id' AND reference.conkey=ARRAY[attribute.attnum]
  WHERE reference.conrelid='control_plane.runs'::regclass AND reference.contype='f'
    AND reference.confrelid='control_plane.workflow_versions'::regclass AND reference.convalidated AND reference.conenforced
    AND NOT EXISTS (SELECT 1 FROM pg_trigger trigger_record
      WHERE trigger_record.tgconstraint=reference.oid AND trigger_record.tgenabled<>'O')),
 'scopeFunctionReady',EXISTS (
  SELECT 1 FROM pg_proc function
  WHERE function.oid=to_regprocedure('control_plane.workspace_session_scope_matches(uuid,uuid,uuid)')
    AND md5(function.prosrc)='d5803f5ad90a6ff3214138ec4de8129c' AND function.proowner='control_plane_owner'::regrole
    AND NOT function.prosecdef AND function.provolatile='s' AND function.proconfig=ARRAY['search_path=pg_catalog']
    AND function.prorettype='boolean'::regtype
    AND function.prolang=(SELECT oid FROM pg_language WHERE lanname IN ('sql'))
    AND has_function_privilege('control_plane_runtime',function.oid,'EXECUTE')
    AND NOT EXISTS (SELECT 1 FROM aclexplode(coalesce(function.proacl,acldefault('f',function.proowner))) acl
      WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 ),
 'workspaceGuardReady',EXISTS (
  SELECT 1 FROM pg_proc function
  WHERE function.oid=to_regprocedure('control_plane.guard_session_workspace_storage()')
    AND md5(function.prosrc)='cea70dfdf75d0fe5c9a943bfc884bcb2' AND function.proowner='control_plane_owner'::regrole
    AND NOT function.prosecdef AND function.provolatile='v' AND function.proconfig=ARRAY['search_path=pg_catalog']
    AND function.prorettype='trigger'::regtype
    AND EXISTS (SELECT 1 FROM pg_trigger trigger_record
      WHERE trigger_record.tgrelid='control_plane.session_workspace_storage'::regclass
        AND trigger_record.tgname='session_workspace_storage_guard' AND NOT trigger_record.tgisinternal
        AND trigger_record.tgenabled='O' AND trigger_record.tgfoid=function.oid)
    AND function.prolang=(SELECT oid FROM pg_language WHERE lanname IN ('plpgsql'))
    AND has_function_privilege('control_plane_runtime',function.oid,'EXECUTE')
    AND NOT EXISTS (SELECT 1 FROM aclexplode(coalesce(function.proacl,acldefault('f',function.proowner))) acl
      WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 ),
 'archiveTaskGuardReady',EXISTS (
  SELECT 1 FROM pg_proc function
  WHERE function.oid=to_regprocedure('control_plane.guard_workspace_archive_task()')
    AND md5(function.prosrc)='3655b43600ba95332c11ed4926a2f948' AND function.proowner='control_plane_owner'::regrole
    AND NOT function.prosecdef AND function.provolatile='v' AND function.proconfig=ARRAY['search_path=pg_catalog']
    AND function.prorettype='trigger'::regtype
    AND EXISTS (SELECT 1 FROM pg_trigger trigger_record
      WHERE trigger_record.tgrelid='control_plane.session_archive_tasks'::regclass
        AND trigger_record.tgname='workspace_archive_task_guard' AND NOT trigger_record.tgisinternal
        AND trigger_record.tgenabled='O' AND trigger_record.tgfoid=function.oid)
    AND has_function_privilege('control_plane_runtime',function.oid,'EXECUTE')
    AND NOT EXISTS (SELECT 1 FROM aclexplode(coalesce(function.proacl,acldefault('f',function.proowner))) acl
      WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 )
)::text;
ROLLBACK;
