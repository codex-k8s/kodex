-- name: chat_repositories__insert :exec
insert into matter_codex_chat_repositories(chat_id, repository_id)
select $1, $2
where not exists (
	select 1 from matter_codex_chat_repositories
	where chat_id = $1 and repository_id = $2
)
on conflict (chat_id, repository_id) do nothing;
