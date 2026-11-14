-- Read-only owner drain после полного postcondition целевой схемы.
-- Никаких business payload, credentials либо изменения leases.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='15s'; SET LOCAL lock_timeout='500ms'; SET LOCAL search_path=pg_catalog;
SELECT jsonb_build_object('profile','PORTABLE_COMPONENT_DRAIN_V1','database',current_database(),
 'readOnly',current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
 'activeRuntimeLeases',(SELECT count(*) FROM control_plane.runtime_leases WHERE state='CLAIMED'),
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
,
 'activeIntegrationInvocations',(SELECT count(*) FROM control_plane.integration_invocations WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED','REJECTED','EXPIRED')),
 'activeIntegrationConnectionTests',(SELECT count(*) FROM control_plane.integration_connection_tests WHERE state IN ('DUE','CLAIMED')),
 'activePersonalCredentialTests',(SELECT count(*) FROM control_plane.integration_personal_credential_tests WHERE state IN ('DUE','CLAIMED','PROBING')),
 'activeRepositoryBundles',(SELECT count(*) FROM control_plane.repository_bundle_publications WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')),
 'activeConfigurationSources',(SELECT count(*) FROM control_plane.managed_configuration_source_work WHERE state IN ('QUEUED','CLAIMED')),
 'activeConfigurationWritebacks',(SELECT count(*) FROM control_plane.managed_configuration_writebacks WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')))::text;
ROLLBACK;
