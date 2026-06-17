-- name: chat_repositories__insert :exec
insert into matter_codex_chat_repositories(chat_id, repository_id)
values ($1, $2)
on conflict (chat_id, repository_id) do nothing;
