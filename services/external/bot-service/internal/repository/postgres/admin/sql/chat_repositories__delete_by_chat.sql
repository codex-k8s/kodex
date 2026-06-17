-- name: chat_repositories__delete_by_chat :exec
delete from matter_codex_chat_repositories
where chat_id = $1;
