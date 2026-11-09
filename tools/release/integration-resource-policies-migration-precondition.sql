-- Предусловия обычного forward update; полный каталог прошлой схемы не копируется.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '500ms';
SET LOCAL search_path = pg_catalog;
WITH migration_tables AS (
    SELECT c.oid, c.relkind, c.relpersistence, c.relispartition, c.relowner, c.relacl FROM pg_class c WHERE c.oid IN (
        'control_plane.integration_approval_scopes'::regclass,
        'control_plane.integration_grants'::regclass,
        'control_plane.integration_invocations'::regclass,
        'control_plane.integration_personal_invocation_credentials'::regclass)
), backup_reader AS (
    SELECT r.oid, r.rolcanlogin, r.rolsuper, r.rolcreatedb, r.rolcreaterole, r.rolinherit, r.rolreplication, r.rolbypassrls
    FROM pg_roles r WHERE r.rolname = 'kodex_backup_reader'
), backup_acl AS (
    SELECT a.privilege_type, a.is_grantable, a.grantor FROM pg_class c
    CROSS JOIN LATERAL aclexplode(coalesce(c.relacl, acldefault('r', c.relowner))) a
    JOIN backup_reader r ON a.grantee = r.oid
    WHERE c.relnamespace = 'control_plane'::regnamespace
), unsafe_backup_columns AS (
    SELECT 1 FROM pg_attribute c
    JOIN pg_class t ON t.oid = c.attrelid
    CROSS JOIN LATERAL aclexplode(c.attacl) a
    JOIN backup_reader r ON a.grantee = r.oid
    WHERE t.relnamespace = 'control_plane'::regnamespace
        AND (a.privilege_type <> 'SELECT' OR a.is_grantable
             OR a.grantor <> 'control_plane_owner'::regrole)
)
SELECT jsonb_build_object(
    'profile', 'INTEGRATION_RESOURCE_POLICIES_PREDECESSOR_V2',
    'readOnly', current_setting('transaction_read_only'),
    'database', current_database(),
    'gooseVersion', (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
    'migrationTablesReady', (SELECT count(*) = 4 AND bool_and(
        relkind = 'r' AND relpersistence = 'p' AND NOT relispartition
        AND relowner = 'control_plane_owner'::regrole) FROM migration_tables),
    'integrationResourcePoliciesAbsent', NOT EXISTS (
        SELECT 1 FROM pg_attribute WHERE attrelid = 'control_plane.integration_approval_scopes'::regclass
            AND attname IN ('scope_lifetime', 'initiator_id', 'version') AND NOT attisdropped)
        AND to_regprocedure('control_plane.integration_pinned_capabilities(uuid)') IS NULL
        AND to_regprocedure('control_plane.integration_policy_legacy_capabilities(text,text,text,text,text)') IS NULL,
    'authorityProofCounterReady', EXISTS (
        SELECT 1 FROM control_plane.authority_proof_revision_counter c
        JOIN control_plane.installation i USING (singleton)
        WHERE c.singleton AND c.revision >= i.authority_proof_sequence
            AND c.revision <= 9007199254740991),
    'backupReaderConfigured', EXISTS (SELECT 1 FROM backup_reader WHERE rolcanlogin
        AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
        AND NOT rolinherit AND NOT rolreplication AND rolbypassrls),
    'backupReaderSafe', NOT EXISTS (SELECT 1 FROM backup_reader
        WHERE rolsuper OR rolcreatedb OR rolcreaterole OR rolinherit OR rolreplication
            OR NOT rolbypassrls OR NOT rolcanlogin)
        AND NOT EXISTS (SELECT 1 FROM backup_acl
        WHERE privilege_type <> 'SELECT' OR is_grantable
            OR grantor <> 'control_plane_owner'::regrole)
        AND NOT EXISTS (SELECT 1 FROM unsafe_backup_columns)
        AND NOT EXISTS (SELECT 1 FROM pg_auth_members m JOIN backup_reader r ON m.member = r.oid)
        AND NOT EXISTS (SELECT 1 FROM pg_default_acl d
            CROSS JOIN LATERAL aclexplode(d.defaclacl) a
            JOIN backup_reader r ON a.grantee = r.oid
            WHERE d.defaclrole = 'control_plane_owner'::regrole
                AND (d.defaclnamespace <> 'control_plane'::regnamespace OR d.defaclobjtype <> 'r'
                    OR a.privilege_type <> 'SELECT' OR a.is_grantable
                    OR a.grantor <> 'control_plane_owner'::regrole)),
    'oldClaimSessions', (SELECT count(*) FROM pg_stat_activity
        WHERE datname = current_database() AND pid <> pg_backend_pid()
            AND query LIKE '%WITH candidates AS%'
            AND query LIKE '%UPDATE control_plane.outbox_events e SET state=%'),
    'activeRuntimeRuns', (SELECT count(*) FROM control_plane.runs WHERE state IN ('QUEUED','RUNNING','CANCELLING','WAITING_HUMAN')),
    'activeRunNodes', (SELECT count(*) FROM control_plane.run_nodes WHERE state IN ('QUEUED','RUNNING','WAITING')),
    'activeSessionTurns', (SELECT count(*) FROM control_plane.session_turns WHERE state IN ('QUEUED','RUNNING')),
    'claimedRuntimeLeases', (SELECT count(*) FROM control_plane.runtime_leases WHERE state = 'CLAIMED'),
    'openBuilds', (SELECT count(*) FROM control_plane.image_builds WHERE stage NOT IN ('COMPLETED','FAILED','CANCELLED','EXPIRED','DEAD_LETTER')),
    'pendingAdmissions', (SELECT count(*) FROM control_plane.image_artifacts WHERE admission_state IN ('PENDING','CLAIMED')),
    'pendingPromotions', (SELECT count(*) FROM control_plane.role_image_promotion_requests WHERE state IN ('QUEUED','PROMOTING')) +
        (SELECT count(*) FROM control_plane.image_artifacts a WHERE a.promotion_state IN ('CLAIMED','AUTHORIZED')
            AND NOT EXISTS (SELECT 1 FROM control_plane.role_image_promotion_requests r
                WHERE r.id = a.promotion_request_id AND r.image_artifact_id = a.id
                    AND r.organization_id = a.organization_id AND r.project_id IS NOT DISTINCT FROM a.project_id
                    AND r.state IN ('QUEUED','PROMOTING'))),
    'activeArchiveTasks', (SELECT count(*) FROM control_plane.session_archive_tasks WHERE state IN ('READY','CLAIMED')),
    'activeModelCatalogTasks', (SELECT count(*) FROM control_plane.provider_model_catalog_tasks WHERE state IN ('PENDING','CLAIMED')),
    'activeRenewalTasks', (SELECT count(*) FROM control_plane.provider_credential_renewal_tasks WHERE state IN ('CLAIMED','AUTHORIZED','UNKNOWN_OUTCOME')),
    'activeIntegrationInvocations', (SELECT count(*) FROM control_plane.integration_invocations WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED')),
    'activeIntegrationConnectionTests', (SELECT count(*) FROM control_plane.integration_connection_tests WHERE state NOT IN ('SUCCEEDED','FAILED','CANCELLED')),
    'activePersonalCredentialTests', (SELECT count(*) FROM control_plane.integration_personal_credential_tests WHERE state IN ('DUE','CLAIMED','PROBING'))
)::text;
ROLLBACK;
