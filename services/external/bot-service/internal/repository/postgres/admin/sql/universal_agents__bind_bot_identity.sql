-- name: universal_agents__bind_bot_identity :exec
update matter_codex_agents agent
set bot_identity_id = $2,
	record_version = agent.record_version + 1,
	updated_at = now()
from matter_codex_mattermost_bot_identities bot_identity
join matter_codex_agent_roles role
	on role.id = bot_identity.role_id
	and role.project_id = bot_identity.project_id
join matter_codex_workspaces workspace
	on workspace.legacy_project_id = role.project_id
where agent.legacy_agent_role_id = $1
	and agent.managed_by = 'ui'
	and bot_identity.id = $2
	and bot_identity.role_id = $1
	and bot_identity.project_id = $3
	and role.project_id = $3
	and agent.organization_scope = workspace.organization_scope
	and exists (
		select 1
		from matter_codex_role_definitions role_definition
		join matter_codex_instruction_sets instruction_set
			on instruction_set.id = agent.instruction_set_id
			and instruction_set.organization_scope = agent.organization_scope
		join matter_codex_agent_assignments assignment
			on assignment.agent_id = agent.id
			and assignment.organization_scope = agent.organization_scope
			and assignment.workspace_id = workspace.id
			and assignment.room_id is null
		where role_definition.id = agent.role_definition_id
			and role_definition.organization_scope = agent.organization_scope
			and role_definition.legacy_agent_role_id = role.id
			and role_definition.managed_by = 'ui'
			and instruction_set.managed_by = 'ui'
	);
