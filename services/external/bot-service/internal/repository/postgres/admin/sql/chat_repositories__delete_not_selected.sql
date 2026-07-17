-- name: chat_repositories__delete_not_selected :exec
delete from matter_codex_chat_repositories
where chat_id = $1
	and not (repository_id = any($2::bigint[]));
