// Package inventory задаёт единственный закрытый source corpus migration job.
package inventory

// Tables — точный лексикографически упорядоченный набор legacy-таблиц.
// Добавление или отсутствие таблицы требует нового reviewed migration plan.
var Tables = []string{
	"matter_codex_agent_delegation_callback_deliveries",
	"matter_codex_agent_delegation_callback_delivery_manifests",
	"matter_codex_agent_delegations",
	"matter_codex_agent_flows",
	"matter_codex_agent_profiles",
	"matter_codex_agent_prompt_templates",
	"matter_codex_agent_role_runtime_variables",
	"matter_codex_agent_roles",
	"matter_codex_agent_runs",
	"matter_codex_agent_session_turns",
	"matter_codex_agent_sessions",
	"matter_codex_audit_events",
	"matter_codex_automation_audit_events",
	"matter_codex_automation_schedules",
	"matter_codex_chat_participants",
	"matter_codex_chat_repositories",
	"matter_codex_chats",
	"matter_codex_cluster_admin_bindings",
	"matter_codex_cluster_admin_bot_bindings",
	"matter_codex_cluster_admin_delivery_fences",
	"matter_codex_cluster_admin_dependencies",
	"matter_codex_cluster_admin_prompt_templates",
	"matter_codex_cluster_admin_revocations",
	"matter_codex_cluster_admin_runtime_variable_bindings",
	"matter_codex_cluster_admin_session_bindings",
	"matter_codex_cluster_admin_subjects",
	"matter_codex_credentials",
	"matter_codex_github_accounts",
	"matter_codex_interaction_capabilities",
	"matter_codex_mattermost_bot_identities",
	"matter_codex_memory_embeddings",
	"matter_codex_memory_record_versions",
	"matter_codex_memory_records",
	"matter_codex_openai_accounts",
	"matter_codex_owner_attention_requests",
	"matter_codex_policy_revisions",
	"matter_codex_process_runs",
	"matter_codex_process_turns",
	"matter_codex_project_repositories",
	"matter_codex_project_runtime_variables",
	"matter_codex_projects",
	"matter_codex_repositories",
	"matter_codex_role_capabilities",
	"matter_codex_role_relationship_policies",
	"matter_codex_runtime_agent_binding_discoveries",
	"matter_codex_runtime_agent_binding_outbox",
	"matter_codex_schedule_occurrences",
	"matter_codex_scheduled_runs",
	"matter_codex_thread_contexts",
	"matter_codex_work_claims",
}

// Contains не допускает prefix/glob semantics.
func Contains(table string) bool {
	for _, candidate := range Tables {
		if candidate == table {
			return true
		}
	}
	return false
}
