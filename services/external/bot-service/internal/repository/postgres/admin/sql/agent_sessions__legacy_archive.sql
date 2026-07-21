select codex_session_id, session_archive_gzip_base64, created_at
from matter_codex_agent_sessions
where id = $1
	and session_archive_gzip_base64 <> '';
