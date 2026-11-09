-- Readback037: затронутые объекты, права и watermark; без полного исторического каталога.
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
), expected_columns(name, type_name, required, default_expression) AS (VALUES
    ('scope_lifetime', 'text', true, $$'ROOT_RUN'::text$$),
    ('initiator_id', 'uuid', false, NULL),
    ('version', 'bigint', true, '1')
), expected_checks(table_name, name, definition) AS (VALUES
    ('integration_approval_scopes', 'integration_approval_scopes_initiator_check', $check$CHECK (((scope_lifetime = 'ROOT_RUN'::text) OR (initiator_id IS NOT NULL)))$check$),
    ('integration_approval_scopes', 'integration_approval_scopes_max_effects_check', $check$CHECK (((max_effects >= 1) AND (max_effects <= 10000)))$check$),
    ('integration_approval_scopes', 'integration_approval_scopes_scope_lifetime_check', $check$CHECK ((scope_lifetime = ANY (ARRAY['ROOT_RUN'::text, 'INITIATOR_RESOURCE'::text])))$check$),
    ('integration_approval_scopes', 'integration_approval_scopes_scope_paths_check', $check$CHECK (((cardinality(scope_paths) >= 0) AND (cardinality(scope_paths) <= 16)))$check$),
    ('integration_approval_scopes', 'integration_approval_scopes_version_check', $check$CHECK ((version > 0))$check$),
    ('integration_grants', 'integration_grants_approval_scope_paths_check', $check$CHECK (((cardinality(approval_scope_paths) <= 16) AND (octet_length(array_to_string(approval_scope_paths, '$check$ || chr(31) || $check$'::text)) <= 4096) AND ((approval_policy = 'HUMAN_SCOPED'::text) OR (cardinality(approval_scope_paths) = 0))))$check$),
    ('integration_grants', 'integration_grants_resource_kind_check', $check$CHECK ((resource_kind = ANY (ARRAY['SYNTHETIC_JOURNAL'::text, 'GITHUB_REPOSITORY'::text, 'GITLAB_PROJECT'::text, 'GITLAB_SCOPE'::text, 'JIRA_PROJECT'::text, 'CONFLUENCE_SPACE'::text, 'EMAIL_SENDER'::text, 'MATTERMOST_CHANNEL'::text, 'HTTPS_RESOURCE'::text, 'SSH_TARGET'::text])))$check$),
    ('integration_invocations', 'integration_invocations_approval_check', $check$CHECK (((approval_policy = ANY (ARRAY['NONE'::text, 'HUMAN_EACH_EFFECT'::text, 'HUMAN_SCOPED'::text])) AND ((approval_policy <> 'NONE'::text) OR (state <> 'WAITING_APPROVAL'::text) OR mailbox_gate_required)))$check$),
    ('integration_invocations', 'integration_invocations_approval_pins_check', $check$CHECK ((((grant_version > 0) AND (((approval_policy <> 'HUMAN_SCOPED'::text) AND (cardinality(approval_scope_paths) = 0)) OR ((approval_policy = 'HUMAN_SCOPED'::text) AND ((cardinality(approval_scope_paths) >= 0) AND (cardinality(approval_scope_paths) <= 16))))) OR ((grant_version = 0) AND (approval_policy <> 'HUMAN_SCOPED'::text) AND (cardinality(approval_scope_paths) = 0) AND (state = ANY (ARRAY['SUCCEEDED'::text, 'FAILED'::text, 'REJECTED'::text, 'CANCELLED'::text])))))$check$),
    ('integration_invocations', 'integration_invocations_resource_kind_check', $check$CHECK ((resource_kind = ANY (ARRAY['SYNTHETIC_JOURNAL'::text, 'GITHUB_REPOSITORY'::text, 'GITLAB_PROJECT'::text, 'GITLAB_SCOPE'::text, 'JIRA_PROJECT'::text, 'CONFLUENCE_SPACE'::text, 'EMAIL_SENDER'::text, 'MATTERMOST_CHANNEL'::text, 'HTTPS_RESOURCE'::text, 'SSH_TARGET'::text])))$check$),
    ('integration_personal_invocation_credentials', 'integration_personal_invocati_resource_permission_profile_check', $check$CHECK ((resource_permission_profile = ANY (ARRAY[''::text, 'CONFLUENCE_SERVER_PAGE_UPDATE_V1'::text, 'JIRA_SERVER_ISSUE_CREATE_V1'::text, 'JIRA_SERVER_COMMENT_WRITE_V1'::text, 'JIRA_SERVER_TRANSITION_APPLY_V1'::text, 'GITLAB_PROJECT_OBJECT_READ_V1'::text, 'JIRA_SERVER_PROJECT_OBJECT_READ_V1'::text, 'CONFLUENCE_SERVER_SPACE_OBJECT_READ_V1'::text, 'GITLAB_PROJECT_ISSUE_CREATE_V1'::text, 'GITLAB_PROJECT_BRANCH_CREATE_V1'::text, 'GITLAB_PROJECT_MERGE_REQUEST_CREATE_V1'::text, 'GITLAB_SCOPED_RESOURCE_V2'::text])))$check$)
), expected_functions(signature, source_md5, volatility, configuration) AS (VALUES
    ('control_plane.guard_integration_approval_scope_insert()', '1d43d8e2c5310643df2fa0b412241c7c', 'v', NULL),
    ('control_plane.guard_integration_approval_scope_update()', '9b2089f5af2b1b58b3190463ed7bd21c', 'v', NULL),
    ('control_plane.guard_personal_invocation_credential()', '25944e08bbab7be2999fdf0f89c765c4', 'v', ARRAY['search_path=pg_catalog, control_plane']),
    ('control_plane.integration_declared_policy_allowed(uuid,text,text,text,text,text,jsonb)', 'c7f9ae006572b19b17615e00b695a20e', 's', ARRAY['search_path=pg_catalog, control_plane']),
    ('control_plane.integration_grant_admission(uuid,uuid,uuid,text,text,text,text,text,text,text[])', 'b8bd5a31b1037aa8cf8011746bd3e534', 's', ARRAY['search_path=pg_catalog, control_plane']),
    ('control_plane.integration_pinned_capabilities(uuid)', 'fca7ae7ef2f9f578e9a577ef6901aa3c', 's', ARRAY['search_path=pg_catalog, control_plane']),
    ('control_plane.integration_policy_legacy_capabilities(text,text,text,text,text)', 'd89ccc0354174e43cd2efcbfc6200f46', 'i', ARRAY['search_path=pg_catalog, control_plane']),
    ('control_plane.protect_email_mailbox_gate()', 'b0f8cb5f7580ccb8b44810887d7ea646', 'v', NULL),
    ('control_plane.revoke_integration_approval_scopes()', 'b2eb6bf2ea7c964e2cfea7fba1fcdcff', 'v', NULL),
    ('control_plane.integration_github_legacy_capabilities(text,text,text,text)', 'ca35090d3f604db55ff1b753f5244c02', 'i', ARRAY['search_path=pg_catalog, control_plane'])
), expected_triggers(table_name, name, definition) AS (VALUES
    ('integration_approval_scopes', 'integration_approval_scopes_guard_insert', $trigger$CREATE TRIGGER integration_approval_scopes_guard_insert BEFORE INSERT ON control_plane.integration_approval_scopes FOR EACH ROW EXECUTE FUNCTION control_plane.guard_integration_approval_scope_insert()$trigger$),
    ('integration_approval_scopes', 'integration_approval_scopes_guard_update', $trigger$CREATE TRIGGER integration_approval_scopes_guard_update BEFORE UPDATE ON control_plane.integration_approval_scopes FOR EACH ROW EXECUTE FUNCTION control_plane.guard_integration_approval_scope_update()$trigger$),
    ('integration_connections', 'integration_approval_scopes_revoke_connection', $trigger$CREATE TRIGGER integration_approval_scopes_revoke_connection AFTER UPDATE ON control_plane.integration_connections FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_integration_approval_scopes()$trigger$),
    ('integration_grants', 'integration_approval_scopes_revoke_grant', $trigger$CREATE TRIGGER integration_approval_scopes_revoke_grant AFTER UPDATE ON control_plane.integration_grants FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_integration_approval_scopes()$trigger$),
    ('integration_invocations', 'email_mailbox_gate_valid', $trigger$CREATE TRIGGER email_mailbox_gate_valid BEFORE INSERT OR UPDATE ON control_plane.integration_invocations FOR EACH ROW EXECUTE FUNCTION control_plane.protect_email_mailbox_gate()$trigger$),
    ('integration_personal_invocation_credentials', 'personal_invocation_credential_owner', $trigger$CREATE TRIGGER personal_invocation_credential_owner BEFORE INSERT ON control_plane.integration_personal_invocation_credentials FOR EACH ROW EXECUTE FUNCTION control_plane.guard_personal_invocation_credential()$trigger$),
    ('runs', 'integration_approval_scopes_revoke_run', $trigger$CREATE TRIGGER integration_approval_scopes_revoke_run AFTER UPDATE OF state ON control_plane.runs FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_integration_approval_scopes()$trigger$)
), backup_tables(name) AS (VALUES
    ('authority_proof_revision_counter'),
    ('integration_approval_scopes'),
    ('integration_connections'),
    ('integration_grants'),
    ('integration_invocations'),
    ('integration_personal_credential_revisions'),
    ('integration_personal_credential_tests'),
    ('integration_personal_credentials'),
    ('integration_personal_invocation_admissions'),
    ('integration_personal_invocation_credentials'),
    ('oidc_identity_observations'),
    ('oidc_identity_policies'),
    ('oidc_identity_policy_current'),
    ('oidc_identity_sessions'),
    ('runs'),
    ('session_archive_tasks'),
    ('session_archives'),
    ('session_workspace_storage'),
    ('session_workspace_volume_attempts'),
    ('session_workspace_volume_tasks'),
    ('session_workspace_volumes')
)
SELECT jsonb_build_object(
    'profile', 'INTEGRATION_RESOURCE_POLICIES_BACKUP_V2',
    'readOnly', current_setting('transaction_read_only'),
    'database', current_database(),
    'gooseVersion', (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied),
    'migrationTablesReady', (SELECT count(*) = 4 AND bool_and(
        relkind = 'r' AND relpersistence = 'p' AND NOT relispartition
        AND relowner = 'control_plane_owner'::regrole) FROM migration_tables),
    'integrationPolicyColumns', NOT EXISTS (
        SELECT 1 FROM expected_columns e LEFT JOIN pg_attribute a
            ON a.attrelid = 'control_plane.integration_approval_scopes'::regclass
                AND a.attname = e.name AND NOT a.attisdropped
        LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
        WHERE a.attnum IS NULL OR a.atttypid <> to_regtype(e.type_name)
            OR a.attnotnull <> e.required OR pg_get_expr(d.adbin, d.adrelid) IS DISTINCT FROM e.default_expression),
    'integrationPolicyConstraints', NOT EXISTS (
        SELECT 1 FROM expected_checks e LEFT JOIN pg_constraint c
            ON c.conrelid = to_regclass('control_plane.' || e.table_name) AND c.conname = e.name
        WHERE c.oid IS NULL OR NOT c.convalidated OR NOT c.conenforced OR c.contype <> 'c'
            OR pg_get_constraintdef(c.oid) <> e.definition),
    'integrationPolicyFunctions', NOT EXISTS (
        SELECT 1 FROM expected_functions e LEFT JOIN pg_proc p ON p.oid = to_regprocedure(e.signature)
        WHERE p.oid IS NULL OR p.proowner <> 'control_plane_owner'::regrole OR p.prosecdef
            OR p.provolatile::text <> e.volatility OR md5(p.prosrc) <> e.source_md5
            OR p.proconfig IS DISTINCT FROM e.configuration
            OR NOT has_function_privilege('control_plane_runtime', p.oid, 'EXECUTE')
            OR EXISTS (SELECT 1 FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a
                WHERE a.is_grantable OR a.privilege_type <> 'EXECUTE'
                    OR a.grantee NOT IN ('control_plane_owner'::regrole, 'control_plane_runtime'::regrole)
                        AND NOT (a.grantee = 0 AND p.prorettype = 'trigger'::regtype))),
    'integrationPolicyGuards', NOT EXISTS (
        SELECT 1 FROM expected_triggers e LEFT JOIN pg_trigger t
            ON t.tgrelid = to_regclass('control_plane.' || e.table_name) AND t.tgname = e.name
        WHERE t.oid IS NULL OR t.tgenabled <> 'O' OR t.tgisinternal OR t.tgdeferrable
            OR pg_get_triggerdef(t.oid) <> e.definition),
    'integrationPolicyPrivileges', NOT EXISTS (SELECT 1 FROM migration_tables c
        CROSS JOIN LATERAL aclexplode(coalesce(c.relacl, acldefault('r', c.relowner))) a WHERE a.grantee = 0),
    'backupReaderReady', NOT EXISTS (SELECT 1 FROM backup_tables t WHERE NOT EXISTS (
        SELECT 1 FROM backup_reader r WHERE has_table_privilege(r.oid, to_regclass('control_plane.' || t.name), 'SELECT'))),
    'workspaceGuards', EXISTS (SELECT 1 FROM pg_trigger t
        WHERE t.tgrelid = 'control_plane.runs'::regclass AND t.tgname = 'workspace_volume_root_closed'
            AND t.tgenabled = 'O' AND t.tgtype = 17 AND NOT t.tgisinternal AND NOT t.tgdeferrable
            AND t.tgfoid = 'control_plane.close_workspace_volume_origin()'::regprocedure
            AND t.tgattr = (SELECT attnum::text::int2vector FROM pg_attribute
                WHERE attrelid = t.tgrelid AND attname = 'state'))
        AND EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conrelid = 'control_plane.session_workspace_volumes'::regclass
            AND c.conname = 'session_workspace_volumes_ready_record_check' AND c.convalidated AND c.conenforced
            AND c.contype = 'c' AND pg_get_constraintdef(c.oid) IN ($check$CHECK (((ready_record IS NULL) OR ((octet_length(ready_record) >= 1) AND (octet_length(ready_record) <= 2048) AND (ready_record IS JSON OBJECT WITH UNIQUE KEYS))))$check$,
                $check$CHECK (((ready_record IS NULL) OR (((octet_length(ready_record) >= 1) AND (octet_length(ready_record) <= 2048)) AND (ready_record IS JSON OBJECT WITH UNIQUE KEYS))))$check$)),
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
                    OR a.grantor <> 'control_plane_owner'::regrole))
)::text;
ROLLBACK;
