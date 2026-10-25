-- name: owner_gate_expiry_migration_precondition :one
-- Только owner metadata для #1869; ни credentials, ни query/payload в результате.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '500ms';
SELECT jsonb_build_object(
    'readOnly', current_setting('transaction_read_only'),
    'database', current_database(),
    'gooseVersion', (SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version),
    'expiryAbsent', NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid='control_plane.owner_gates'::regclass AND attname='expires_at' AND NOT attisdropped),
    'oldClaimSessions', (SELECT count(*) FROM pg_stat_activity
        WHERE datname = current_database() AND pid <> pg_backend_pid()
            AND query LIKE '%WITH candidates AS%'
            AND query LIKE '%UPDATE control_plane.outbox_events e SET state=%'),
    'activeRuntimeRuns', (SELECT count(*) FROM control_plane.runs WHERE state IN ('QUEUED', 'RUNNING', 'CANCELLING', 'WAITING_HUMAN')),
    'activeRunNodes', (SELECT count(*) FROM control_plane.run_nodes WHERE state IN ('QUEUED', 'RUNNING', 'WAITING')),
    'activeSessionTurns', (SELECT count(*) FROM control_plane.session_turns WHERE state IN ('QUEUED', 'RUNNING')),
    'claimedRuntimeLeases', (SELECT count(*) FROM control_plane.runtime_leases WHERE state = 'CLAIMED'),
    'openBuilds', (SELECT count(*) FROM control_plane.image_builds WHERE stage NOT IN ('COMPLETED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DEAD_LETTER')),
    'pendingAdmissions', (SELECT count(*) FROM control_plane.image_artifacts WHERE admission_state IN ('PENDING', 'CLAIMED')),
    'pendingPromotions', (SELECT count(*) FROM control_plane.role_image_promotion_requests WHERE state IN ('QUEUED', 'PROMOTING')) +
        (SELECT count(*) FROM control_plane.image_artifacts artifact
            WHERE artifact.promotion_state IN ('CLAIMED', 'AUTHORIZED') AND NOT EXISTS (
                SELECT 1 FROM control_plane.role_image_promotion_requests request
                WHERE request.id = artifact.promotion_request_id AND request.image_artifact_id = artifact.id
                    AND request.organization_id = artifact.organization_id
                    AND request.project_id IS NOT DISTINCT FROM artifact.project_id
                    AND request.state IN ('QUEUED', 'PROMOTING'))),
    'activeArchiveTasks', (SELECT count(*) FROM control_plane.session_archive_tasks WHERE state IN ('READY', 'CLAIMED')),
    'activeModelCatalogTasks', (SELECT count(*) FROM control_plane.provider_model_catalog_tasks WHERE state IN ('PENDING', 'CLAIMED')),
    'activeRenewalTasks', (SELECT count(*) FROM control_plane.provider_credential_renewal_tasks WHERE state IN ('CLAIMED', 'AUTHORIZED', 'UNKNOWN_OUTCOME'))
)::text;
ROLLBACK;
