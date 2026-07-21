insert into matter_codex_agent_session_archives (
	session_id, turn_id, version, codex_session_id, payload_gzip_base64, sha256, size_bytes
) values ($1, $2, $3, $4, $5, $6, $7)
returning id, session_id, coalesce(turn_id, 0), version, codex_session_id, payload_gzip_base64,
	sha256, size_bytes, created_at;
