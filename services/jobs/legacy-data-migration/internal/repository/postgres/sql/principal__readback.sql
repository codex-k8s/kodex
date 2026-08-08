-- name: principal__readback :one
WITH RECURSIVE session_role AS (
    SELECT oid
    FROM pg_catalog.pg_roles
    WHERE rolname = session_user
), required_role AS (
    SELECT role.*
    FROM pg_catalog.pg_roles AS role
    WHERE role.rolname = @required_role
), memberships(roleid) AS (
    SELECT member.roleid
    FROM pg_catalog.pg_auth_members AS member
    JOIN session_role ON session_role.oid = member.member
    UNION
    SELECT parent.roleid
    FROM pg_catalog.pg_auth_members AS parent
    JOIN memberships ON memberships.roleid = parent.member
), source_tables(table_schema, table_name) AS (
    VALUES
        ('public', 'matter_codex_agent_delegation_callback_deliveries'),
        ('public', 'matter_codex_agent_delegation_callback_delivery_manifests'),
        ('public', 'matter_codex_agent_delegations'),
        ('public', 'matter_codex_agent_flows'),
        ('public', 'matter_codex_agent_profiles'),
        ('public', 'matter_codex_agent_prompt_templates'),
        ('public', 'matter_codex_agent_role_runtime_variables'),
        ('public', 'matter_codex_agent_roles'),
        ('public', 'matter_codex_agent_runs'),
        ('public', 'matter_codex_agent_session_turns'),
        ('public', 'matter_codex_agent_sessions'),
        ('public', 'matter_codex_audit_events'),
        ('public', 'matter_codex_automation_audit_events'),
        ('public', 'matter_codex_automation_schedules'),
        ('public', 'matter_codex_chat_participants'),
        ('public', 'matter_codex_chat_repositories'),
        ('public', 'matter_codex_chats'),
        ('public', 'matter_codex_cluster_admin_bindings'),
        ('public', 'matter_codex_cluster_admin_bot_bindings'),
        ('public', 'matter_codex_cluster_admin_delivery_fences'),
        ('public', 'matter_codex_cluster_admin_dependencies'),
        ('public', 'matter_codex_cluster_admin_prompt_templates'),
        ('public', 'matter_codex_cluster_admin_revocations'),
        ('public', 'matter_codex_cluster_admin_runtime_variable_bindings'),
        ('public', 'matter_codex_cluster_admin_session_bindings'),
        ('public', 'matter_codex_cluster_admin_subjects'),
        ('public', 'matter_codex_credentials'),
        ('public', 'matter_codex_github_accounts'),
        ('public', 'matter_codex_interaction_capabilities'),
        ('public', 'matter_codex_mattermost_bot_identities'),
        ('public', 'matter_codex_memory_embeddings'),
        ('public', 'matter_codex_memory_record_versions'),
        ('public', 'matter_codex_memory_records'),
        ('public', 'matter_codex_openai_accounts'),
        ('public', 'matter_codex_owner_attention_requests'),
        ('public', 'matter_codex_policy_revisions'),
        ('public', 'matter_codex_process_runs'),
        ('public', 'matter_codex_process_turns'),
        ('public', 'matter_codex_project_repositories'),
        ('public', 'matter_codex_project_runtime_variables'),
        ('public', 'matter_codex_projects'),
        ('public', 'matter_codex_repositories'),
        ('public', 'matter_codex_role_capabilities'),
        ('public', 'matter_codex_role_relationship_policies'),
        ('public', 'matter_codex_runtime_agent_binding_discoveries'),
        ('public', 'matter_codex_runtime_agent_binding_outbox'),
        ('public', 'matter_codex_schedule_occurrences'),
        ('public', 'matter_codex_scheduled_runs'),
        ('public', 'matter_codex_thread_contexts'),
        ('public', 'matter_codex_work_claims'),
        ('public', 'matter_codex_legacy_data_cutovers')
)
SELECT current_user = session_user,
       role.rolcanlogin
       AND NOT role.rolsuper
       AND NOT role.rolcreatedb
       AND NOT role.rolcreaterole
       AND NOT role.rolreplication
       AND NOT role.rolbypassrls
       AND NOT EXISTS (
           SELECT 1
           FROM memberships
           JOIN pg_catalog.pg_roles AS inherited ON inherited.oid = memberships.roleid
           WHERE @required_role = '' OR inherited.rolname <> @required_role
       )
       AND (
           @required_role = ''
           OR NOT EXISTS (
               SELECT 1
               FROM information_schema.tables AS candidate
               WHERE candidate.table_schema NOT IN ('pg_catalog', 'information_schema')
                 AND NOT (
                     candidate.table_schema = 'public'
                     AND candidate.table_name = 'matter_codex_legacy_data_cutovers'
                 )
                 AND (
                     has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'INSERT')
                     OR has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'UPDATE')
                     OR has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'DELETE')
                     OR has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'TRUNCATE')
                     OR has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'REFERENCES')
                     OR has_table_privilege(session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name), 'TRIGGER')
               )
           )
       )
       AND (
           @required_role = ''
           OR NOT EXISTS (
               SELECT 1
               FROM pg_catalog.pg_namespace AS namespace
               WHERE namespace.nspname <> 'pg_catalog'
                 AND namespace.nspname <> 'information_schema'
                 AND namespace.nspname !~ '^pg_toast'
                 AND namespace.nspname !~ '^pg_temp_'
                 AND has_schema_privilege(session_user, namespace.oid, 'CREATE')
           )
       )
       AND CASE @required_role
           WHEN 'matter_codex_migration' THEN
               NOT EXISTS (
                   SELECT 1 FROM source_tables AS expected
                   WHERE NOT has_table_privilege(
                       session_user,
                       format('%I.%I', expected.table_schema, expected.table_name),
                       'SELECT'
                   )
               )
               AND NOT EXISTS (
                   SELECT 1 FROM information_schema.tables AS candidate
                   WHERE candidate.table_schema NOT IN ('pg_catalog', 'information_schema')
                     AND has_table_privilege(
                         session_user,
                         format('%I.%I', candidate.table_schema, candidate.table_name),
                         'SELECT'
                     )
                     AND NOT EXISTS (
                         SELECT 1 FROM source_tables AS expected
                         WHERE expected.table_schema = candidate.table_schema
                           AND expected.table_name = candidate.table_name
                     )
               )
               AND has_function_privilege(
                   session_user, 'public.matter_codex_legacy_snapshot_rows()', 'EXECUTE'
               )
               AND has_function_privilege(
                   session_user, 'public.matter_codex_lock_legacy_business_tables()', 'EXECUTE'
               )
           ELSE true
       END,
       CASE WHEN @required_role = '' THEN true
            ELSE EXISTS (
                     SELECT 1
                     FROM required_role
                     WHERE NOT required_role.rolcanlogin
                       AND NOT required_role.rolsuper
                       AND NOT required_role.rolcreatedb
                       AND NOT required_role.rolcreaterole
                       AND NOT required_role.rolreplication
                       AND NOT required_role.rolbypassrls
                 )
                 AND pg_has_role(session_user, @required_role, 'usage')
                 AND (SELECT count(*) FROM memberships) = 1
       END
FROM pg_catalog.pg_roles AS role
WHERE role.rolname = session_user;
