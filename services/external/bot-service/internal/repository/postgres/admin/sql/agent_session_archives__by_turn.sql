select id, session_id, coalesce(turn_id, 0), version, codex_session_id, payload_gzip_base64,
	sha256, size_bytes, created_at
from matter_codex_agent_session_archives
where session_id = $1 and turn_id = $2;
-- name: agent_session_archives__by_turn :one
