-- name: approval__decide :exec
update matter_codex_approval_requests
set state = $2, decision_actor_user_id = $3, decision_actor_user_name = $4,
	decision_at = $5, decision_reason = $6, updated_at = $5
where id = $1 and state = 'pending'
	and exact_approver_user_id = $3
	and not exists(
		select 1 from matter_codex_mattermost_bot_identities where mattermost_user_id = $3
	);
