-- name: chat_participants__delete_not_selected :exec
delete from matter_codex_chat_participants
where chat_id = $1
	and not (role_id = any($2::bigint[]));
