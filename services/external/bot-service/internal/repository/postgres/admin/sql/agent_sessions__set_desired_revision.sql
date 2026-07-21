update matter_codex_agent_sessions
set desired_runtime_revision_id = $2,
	updated_at = now()
where session_key = $1
returning
	id,
	session_key,
	coalesce(desired_runtime_revision_id, 0),
	coalesce(applied_runtime_revision_id, 0);
