select sessions.status = 'idle'
	and sessions.active_turn_id is null
	and sessions.pod_name = $2
	and not exists (
		select 1
		from matter_codex_agent_session_turns turns
		where turns.session_id = sessions.id
			and turns.status in ('queued', 'running')
	)
from matter_codex_agent_sessions sessions
where sessions.session_key = $1;
