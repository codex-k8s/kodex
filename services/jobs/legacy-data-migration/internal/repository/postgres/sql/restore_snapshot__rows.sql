-- name: restore_snapshot__rows :many
SELECT restored.table_name, restored.row_payload
FROM (
    SELECT 'matter_codex_agent_delegation_callback_deliveries'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_delegation_callback_deliveries'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_delegation_callback_deliveries AS source_row
    UNION ALL
    SELECT 'matter_codex_agent_delegation_callback_delivery_manifests'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_delegation_callback_delivery_manifests'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_delegation_callback_delivery_manifests AS source_row
    UNION ALL
    SELECT 'matter_codex_agent_delegations'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_delegations'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_delegations AS source_row
    UNION ALL
    SELECT 'matter_codex_agent_flows'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_flows'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_flows AS source_row
    UNION ALL
    SELECT 'matter_codex_agent_profiles'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_profiles'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_profiles AS source_row
    UNION ALL
    SELECT 'matter_codex_agent_prompt_templates'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_prompt_templates'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_prompt_templates AS source_row
    UNION ALL
    SELECT 'matter_codex_agent_role_runtime_variables'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_role_runtime_variables'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_role_runtime_variables AS source_row
    UNION ALL
    SELECT 'matter_codex_agent_roles'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_roles'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_roles AS source_row
    UNION ALL
    SELECT 'matter_codex_agent_runs'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_runs'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_runs AS source_row
    UNION ALL
    SELECT 'matter_codex_agent_session_turns'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_session_turns'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_session_turns AS source_row
    UNION ALL
    SELECT 'matter_codex_agent_sessions'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_agent_sessions'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_agent_sessions AS source_row
    UNION ALL
    SELECT 'matter_codex_audit_events'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_audit_events'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_audit_events AS source_row
    UNION ALL
    SELECT 'matter_codex_automation_audit_events'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_automation_audit_events'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_automation_audit_events AS source_row
    UNION ALL
    SELECT 'matter_codex_automation_schedules'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_automation_schedules'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_automation_schedules AS source_row
    UNION ALL
    SELECT 'matter_codex_chat_participants'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_chat_participants'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_chat_participants AS source_row
    UNION ALL
    SELECT 'matter_codex_chat_repositories'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_chat_repositories'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_chat_repositories AS source_row
    UNION ALL
    SELECT 'matter_codex_chats'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_chats'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_chats AS source_row
    UNION ALL
    SELECT 'matter_codex_cluster_admin_bindings'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_cluster_admin_bindings'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_cluster_admin_bindings AS source_row
    UNION ALL
    SELECT 'matter_codex_cluster_admin_bot_bindings'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_cluster_admin_bot_bindings'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_cluster_admin_bot_bindings AS source_row
    UNION ALL
    SELECT 'matter_codex_cluster_admin_delivery_fences'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_cluster_admin_delivery_fences'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_cluster_admin_delivery_fences AS source_row
    UNION ALL
    SELECT 'matter_codex_cluster_admin_dependencies'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_cluster_admin_dependencies'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_cluster_admin_dependencies AS source_row
    UNION ALL
    SELECT 'matter_codex_cluster_admin_prompt_templates'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_cluster_admin_prompt_templates'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_cluster_admin_prompt_templates AS source_row
    UNION ALL
    SELECT 'matter_codex_cluster_admin_revocations'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_cluster_admin_revocations'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_cluster_admin_revocations AS source_row
    UNION ALL
    SELECT 'matter_codex_cluster_admin_runtime_variable_bindings'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_cluster_admin_runtime_variable_bindings'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_cluster_admin_runtime_variable_bindings AS source_row
    UNION ALL
    SELECT 'matter_codex_cluster_admin_session_bindings'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_cluster_admin_session_bindings'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_cluster_admin_session_bindings AS source_row
    UNION ALL
    SELECT 'matter_codex_cluster_admin_subjects'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_cluster_admin_subjects'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_cluster_admin_subjects AS source_row
    UNION ALL
    SELECT 'matter_codex_credentials'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_credentials'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_credentials AS source_row
    UNION ALL
    SELECT 'matter_codex_github_accounts'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_github_accounts'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_github_accounts AS source_row
    UNION ALL
    SELECT 'matter_codex_interaction_capabilities'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_interaction_capabilities'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_interaction_capabilities AS source_row
    UNION ALL
    SELECT 'matter_codex_mattermost_bot_identities'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_mattermost_bot_identities'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_mattermost_bot_identities AS source_row
    UNION ALL
    SELECT 'matter_codex_memory_embeddings'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_memory_embeddings'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_memory_embeddings AS source_row
    UNION ALL
    SELECT 'matter_codex_memory_record_versions'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_memory_record_versions'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_memory_record_versions AS source_row
    UNION ALL
    SELECT 'matter_codex_memory_records'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_memory_records'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_memory_records AS source_row
    UNION ALL
    SELECT 'matter_codex_openai_accounts'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_openai_accounts'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_openai_accounts AS source_row
    UNION ALL
    SELECT 'matter_codex_owner_attention_requests'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_owner_attention_requests'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_owner_attention_requests AS source_row
    UNION ALL
    SELECT 'matter_codex_policy_revisions'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_policy_revisions'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_policy_revisions AS source_row
    UNION ALL
    SELECT 'matter_codex_process_runs'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_process_runs'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_process_runs AS source_row
    UNION ALL
    SELECT 'matter_codex_process_turns'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_process_turns'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_process_turns AS source_row
    UNION ALL
    SELECT 'matter_codex_project_repositories'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_project_repositories'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_project_repositories AS source_row
    UNION ALL
    SELECT 'matter_codex_project_runtime_variables'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_project_runtime_variables'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_project_runtime_variables AS source_row
    UNION ALL
    SELECT 'matter_codex_projects'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_projects'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_projects AS source_row
    UNION ALL
    SELECT 'matter_codex_repositories'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_repositories'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_repositories AS source_row
    UNION ALL
    SELECT 'matter_codex_role_capabilities'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_role_capabilities'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_role_capabilities AS source_row
    UNION ALL
    SELECT 'matter_codex_role_relationship_policies'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_role_relationship_policies'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_role_relationship_policies AS source_row
    UNION ALL
    SELECT 'matter_codex_runtime_agent_binding_discoveries'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_runtime_agent_binding_discoveries'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_runtime_agent_binding_discoveries AS source_row
    UNION ALL
    SELECT 'matter_codex_runtime_agent_binding_outbox'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_runtime_agent_binding_outbox'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_runtime_agent_binding_outbox AS source_row
    UNION ALL
    SELECT 'matter_codex_schedule_occurrences'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_schedule_occurrences'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_schedule_occurrences AS source_row
    UNION ALL
    SELECT 'matter_codex_scheduled_runs'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_scheduled_runs'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_scheduled_runs AS source_row
    UNION ALL
    SELECT 'matter_codex_thread_contexts'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_thread_contexts'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_thread_contexts AS source_row
    UNION ALL
    SELECT 'matter_codex_work_claims'::text AS table_name, NULL::text AS row_payload
    UNION ALL
    SELECT 'matter_codex_work_claims'::text, to_jsonb(source_row)::text
    FROM public.matter_codex_work_claims AS source_row
) AS restored
ORDER BY restored.table_name, restored.row_payload COLLATE "C" NULLS FIRST;
