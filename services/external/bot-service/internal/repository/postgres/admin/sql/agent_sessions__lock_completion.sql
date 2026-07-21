select id, archive_version, codex_session_id, session_archive_gzip_base64
from matter_codex_agent_sessions
where session_key = $1
for update;
