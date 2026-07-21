-- name: approval__expire :exec
update matter_codex_approval_requests
set state = 'expired', decision_actor_user_id = 'matter-codex-system',
	decision_actor_user_name = 'matter-codex', decision_at = $2,
	decision_reason = 'approval.expired', updated_at = $2
where id = $1 and state = 'pending' and expires_at <= $2;
