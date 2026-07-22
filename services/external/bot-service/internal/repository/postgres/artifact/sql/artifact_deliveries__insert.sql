-- name: artifact_deliveries__insert :exec
insert into matter_codex_artifact_deliveries(
	id, artifact_version_id, project_id, chat_id, session_id, role_id, runtime_turn_id, turn_id,
	idempotency_key, bot_token_secret_ref, state, error_code
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);
