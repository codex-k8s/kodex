BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object(
 'profile','WORKSPACE_UNBOUND_STOP_V1','database',current_database(),'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'workspaceGuardReady',EXISTS(SELECT 1 FROM pg_proc WHERE oid='control_plane.guard_session_workspace_storage()'::regprocedure
  AND proowner='control_plane_owner'::regrole AND NOT prosecdef AND proconfig=ARRAY['search_path=pg_catalog'] AND md5(prosrc)='452c9a0632bcfe184e9896ef246fb97a'),
 'receiptGuardReady',EXISTS(SELECT 1 FROM pg_proc WHERE oid='control_plane.guard_workspace_unbound_stop()'::regprocedure
  AND proowner='control_plane_owner'::regrole AND NOT prosecdef AND proconfig=ARRAY['search_path=pg_catalog'] AND md5(prosrc)='4f58c76ae4d3a7e4468c1b713fff4ee6'),
 'receiptTableReady',(SELECT count(*)=3 AND bool_and(condeferrable AND NOT condeferred) FROM pg_constraint WHERE conrelid='control_plane.workspace_unbound_stops'::regclass AND contype='f') AND EXISTS(SELECT 1 FROM pg_class WHERE oid='control_plane.workspace_unbound_stops'::regclass AND relowner='control_plane_owner'::regrole)
  AND has_table_privilege('control_plane_runtime','control_plane.workspace_unbound_stops','SELECT')
  AND has_table_privilege('control_plane_runtime','control_plane.workspace_unbound_stops','INSERT')
  AND NOT has_table_privilege('control_plane_runtime','control_plane.workspace_unbound_stops','UPDATE')
  AND NOT has_table_privilege('control_plane_runtime','control_plane.workspace_unbound_stops','DELETE')
  AND NOT has_table_privilege('control_plane_runtime','control_plane.workspace_unbound_stops','TRUNCATE'),
 'receiptTriggerReady',EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='control_plane.workspace_unbound_stops'::regclass
  AND tgname='guard_workspace_unbound_stop' AND tgfoid='control_plane.guard_workspace_unbound_stop()'::regprocedure AND tgenabled='O' AND NOT tgisinternal),
 'archiveGuardPreserved',EXISTS(SELECT 1 FROM pg_proc WHERE oid='control_plane.guard_workspace_archive_task()'::regprocedure AND md5(prosrc)='18927e175e85b3441e667b62fec8044c')
)::text;
ROLLBACK;
