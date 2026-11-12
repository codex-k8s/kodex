BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='500ms';
SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object(
 'profile','WORKSPACE_ARCHIVE_ABI5_PREDECESSOR_V1','database',current_database(),
 'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'archiveGuardPredecessor',EXISTS(SELECT 1 FROM pg_proc p
  WHERE p.oid='control_plane.guard_workspace_archive_task()'::regprocedure
   AND md5(p.prosrc)='3655b43600ba95332c11ed4926a2f948'
   AND p.proowner='control_plane_owner'::regrole AND NOT p.prosecdef AND p.provolatile='v'
   AND p.prorettype='trigger'::regtype AND NOT p.proretset AND p.pronargs=0
   AND p.proconfig=ARRAY['search_path=pg_catalog']
   AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')),
 'activeRuntimeLeases',(SELECT count(*) FROM control_plane.runtime_leases WHERE state='CLAIMED' AND expires_at>clock_timestamp()),
 'activeArchiveTasks',(SELECT count(*) FROM control_plane.session_archive_tasks WHERE state IN ('READY','CLAIMED')),
 'activeRuntimeRuns',(SELECT count(*) FROM control_plane.runs WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED')),
 'activeRunNodes',(SELECT count(*) FROM control_plane.run_nodes WHERE state IN ('QUEUED','RUNNING','WAITING')),
 'activeSessionTurns',(SELECT count(*) FROM control_plane.session_turns WHERE state IN ('QUEUED','RUNNING')),
 'claimedRuntimeLeases',(SELECT count(*) FROM control_plane.runtime_leases WHERE state='CLAIMED'),
 'openBuilds',(SELECT count(*) FROM control_plane.image_builds WHERE stage NOT IN ('COMPLETED','FAILED','CANCELLED','EXPIRED','DEAD_LETTER')),
 'pendingAdmissions',(SELECT count(*) FROM control_plane.image_artifacts WHERE admission_state IN ('PENDING','CLAIMED')),
 'pendingPromotions',(SELECT count(*) FROM control_plane.role_image_promotion_requests WHERE state IN ('QUEUED','PROMOTING'))+
  (SELECT count(*) FROM control_plane.image_artifacts a WHERE a.promotion_state IN ('CLAIMED','AUTHORIZED')
   AND NOT EXISTS(SELECT 1 FROM control_plane.role_image_promotion_requests r WHERE r.id=a.promotion_request_id
    AND r.image_artifact_id=a.id AND r.organization_id=a.organization_id AND r.project_id IS NOT DISTINCT FROM a.project_id
    AND r.state IN ('QUEUED','PROMOTING'))),
 'activeModelCatalogTasks',(SELECT count(*) FROM control_plane.provider_model_catalog_tasks WHERE state IN ('PENDING','CLAIMED')),
 'activeRenewalTasks',(SELECT count(*) FROM control_plane.provider_credential_renewal_tasks WHERE state IN ('CLAIMED','AUTHORIZED','UNKNOWN_OUTCOME')),
 'oldClaimSessions',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()
  AND query LIKE '%WITH candidates AS%' AND query LIKE '%UPDATE control_plane.outbox_events e SET state=%')
)::text;
ROLLBACK;
