-- name: artifacts__insert :exec
insert into matter_codex_artifacts(
	id, project_id, chat_id, session_id, role_id, runtime_turn_id, turn_id, direction,
	mattermost_post_id, mattermost_file_id, retention_until
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);
