-- name: approval__insert :one
insert into matter_codex_approval_requests(
	public_id, invocation_id, approval_binding_sha256, state, risk_class, safe_preview,
	exact_approver_user_id, exact_approver_user_name, expires_at,
	mattermost_channel_id, mattermost_root_post_id, created_at, updated_at
)
values ($1, $2, $3, 'pending', 'platform_admin', $4::jsonb, $5, $6, $7, $8, $9, $10, $10)
returning id;
