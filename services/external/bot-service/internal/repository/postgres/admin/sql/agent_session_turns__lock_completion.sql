select session_id, status, run_id, coalesce(runtime_revision_id, 0),
	final_message, error_message, artifacts::text, completion_pod_uid
from matter_codex_agent_session_turns
where id = $1
for update;
