-- name: chat_participants__insert :exec
insert into matter_codex_chat_participants(chat_id, role_id)
values ($1, $2)
on conflict (chat_id, role_id) do update set enabled = true;
