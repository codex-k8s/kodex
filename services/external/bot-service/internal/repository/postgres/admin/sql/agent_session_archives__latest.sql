select id, session_id, version, codex_session_id, payload_gzip_base64,
	sha256, size_bytes, created_at
from matter_codex_agent_session_archives
where session_id = $1
order by version desc
limit 1;
