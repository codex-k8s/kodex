select
	id,
	session_key,
	coalesce(desired_runtime_revision_id, 0),
	coalesce(applied_runtime_revision_id, 0)
from matter_codex_agent_sessions
where session_key = $1;
