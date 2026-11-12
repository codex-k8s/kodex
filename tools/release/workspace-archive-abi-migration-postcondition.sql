BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object(
 'profile','WORKSPACE_ARCHIVE_ABI5_V1','database',current_database(),
 'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'archiveGuardReady',EXISTS(SELECT 1 FROM pg_proc p
  WHERE p.oid='control_plane.guard_workspace_archive_task()'::regprocedure
   AND md5(p.prosrc)='18927e175e85b3441e667b62fec8044c'
   AND p.proowner='control_plane_owner'::regrole AND NOT p.prosecdef AND p.provolatile='v'
   AND p.prorettype='trigger'::regtype AND NOT p.proretset AND p.pronargs=0
   AND p.proconfig=ARRAY['search_path=pg_catalog']
   AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
   AND has_function_privilege('control_plane_runtime',p.oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
    WHERE a.grantee NOT IN('control_plane_owner'::regrole,'control_plane_runtime'::regrole)
     OR a.grantee='control_plane_runtime'::regrole AND (a.is_grantable OR a.privilege_type<>'EXECUTE'))),
 'archiveTriggerReady',EXISTS(SELECT 1 FROM pg_trigger t
  WHERE t.tgrelid='control_plane.session_archive_tasks'::regclass AND t.tgname='workspace_archive_task_guard'
   AND t.tgfoid='control_plane.guard_workspace_archive_task()'::regprocedure
   AND t.tgenabled='O' AND NOT t.tgisinternal AND t.tgtype=31 AND t.tgnargs=0 AND t.tgqual IS NULL)
)::text;
ROLLBACK;
