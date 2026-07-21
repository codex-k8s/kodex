select
	session_row.id,
	session_row.session_key,
	session_row.project_id,
	session_row.chat_id,
	session_row.role_id,
	coalesce(session_row.active_turn_id, 0),
	session_row.active_run_id,
	session_row.status,
	turn_row.id,
	turn_row.session_id,
	turn_row.run_id,
	turn_row.status,
	turn_row.mattermost_channel_id,
	turn_row.mattermost_root_post_id,
	turn_row.mattermost_post_id
from matter_codex_agent_sessions session_row
join matter_codex_agent_session_turns turn_row
	on turn_row.session_id = session_row.id
	and turn_row.id = $2
where session_row.id = $1
for update of session_row, turn_row;
