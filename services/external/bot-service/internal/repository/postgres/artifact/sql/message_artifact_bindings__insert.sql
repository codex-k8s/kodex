-- name: message_artifact_bindings__insert :exec
insert into matter_codex_message_artifact_bindings(
	artifact_version_id, project_id, chat_id, session_id, turn_id,
	mattermost_post_id, mattermost_file_id, direction, ordinal
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
on conflict (artifact_version_id, project_id, chat_id, session_id, turn_id) do nothing;
