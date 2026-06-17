-- name: chat_participants__delete_by_chat :exec
delete from matter_codex_chat_participants
where chat_id = $1;
