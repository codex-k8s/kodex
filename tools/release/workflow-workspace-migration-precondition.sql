BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object(
 'profile','WORKFLOW_WORKSPACE_PREDECESSOR_V1',
 'database',current_database(), 'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'workflowWorkspaceAbsent',NOT EXISTS (SELECT 1 FROM pg_attribute
  WHERE attrelid='control_plane.session_workspace_storage'::regclass AND attname='workflow_version_id' AND NOT attisdropped),
 'activeRuntimeRuns',(SELECT count(*) FROM control_plane.runs WHERE state IN ('QUEUED','RUNNING','CANCELLING','WAITING_HUMAN')),
 'activeRunNodes',(SELECT count(*) FROM control_plane.run_nodes WHERE state IN ('QUEUED','RUNNING','WAITING')),
 'activeSessionTurns',(SELECT count(*) FROM control_plane.session_turns WHERE state IN ('QUEUED','RUNNING')),
 'claimedRuntimeLeases',(SELECT count(*) FROM control_plane.runtime_leases WHERE state='CLAIMED'),
 'activeWorkspaceVolumeTasks',(SELECT count(*) FROM control_plane.session_workspace_volume_tasks WHERE state IN ('READY','CLAIMED')),
 'activeWorkspaceArchiveTasks',(SELECT count(*) FROM control_plane.session_archive_tasks WHERE workspace_id IS NOT NULL AND state IN ('READY','CLAIMED')),
 'workspaceGuardReady',EXISTS (
  SELECT 1 FROM pg_proc function
  WHERE function.oid=to_regprocedure('control_plane.guard_session_workspace_storage()')
    AND md5(function.prosrc)='e4aed9f09b46c006f08aecea011ee8c6' AND function.proowner='control_plane_owner'::regrole
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
    AND md5(function.prosrc)='93a6276029ab000b40c090d846748cad' AND function.proowner='control_plane_owner'::regrole
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
