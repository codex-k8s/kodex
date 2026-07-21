-- name: universal_agent_assignments__room_disable :exec
update matter_codex_agent_assignments assignment
set enabled = false, updated_at = now()
from matter_codex_rooms room
where assignment.room_id = room.id
	and room.legacy_chat_id = $1
	and not exists (
		select 1
		from matter_codex_agents agent
		where agent.id = assignment.agent_id
			and agent.legacy_agent_role_id = any($2::bigint[])
	);
