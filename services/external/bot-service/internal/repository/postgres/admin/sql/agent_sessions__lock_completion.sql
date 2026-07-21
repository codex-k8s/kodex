select id, archive_version, codex_session_id, session_archive_gzip_base64,
	coalesce(active_turn_id, 0), active_run_id,
	coalesce(applied_runtime_revision_id, 0), applied_pod_uid
from matter_codex_agent_sessions
where session_key = $1
for update;
