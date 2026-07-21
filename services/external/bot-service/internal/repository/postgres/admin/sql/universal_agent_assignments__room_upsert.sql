-- name: universal_agent_assignments__room_upsert :exec
insert into matter_codex_agent_assignments(
	organization_scope, agent_id, workspace_id, room_id, enabled, is_default
)
select agent.organization_scope, agent.id, room.workspace_id, room.id, true, $3
from matter_codex_agents agent
join matter_codex_rooms room on room.legacy_chat_id = $2
where agent.legacy_agent_role_id = $1
on conflict (agent_id, workspace_id, room_id) do update set
	enabled = true,
	is_default = excluded.is_default,
	updated_at = now();
