-- name: universal_agent_assignments__workspace_upsert :exec
insert into matter_codex_agent_assignments(
	organization_scope, agent_id, workspace_id, room_id, enabled, is_default
)
select agent.organization_scope, agent.id, workspace.id, null, $3, false
from matter_codex_agents agent
join matter_codex_workspaces workspace on workspace.legacy_project_id = $2
where agent.legacy_agent_role_id = $1
on conflict (agent_id, workspace_id, room_id) do update set
	enabled = excluded.enabled,
	updated_at = now();
