insert into matter_codex_agent_session_archives (
	session_id, version, codex_session_id, payload_gzip_base64, sha256, size_bytes
) values ($1, $2, $3, $4, $5, $6)
returning id, session_id, version, codex_session_id, payload_gzip_base64,
	sha256, size_bytes, created_at;
