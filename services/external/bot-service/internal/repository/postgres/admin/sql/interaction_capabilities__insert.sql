-- name: interaction_capabilities__insert :exec
insert into matter_codex_interaction_capabilities (
	token_hash,
	kind,
	operation,
	resource_type,
	resource_id,
	channel_id,
	post_binding,
	actor_user_id,
	actor_user_name,
	installation_scope,
	workspace_scope,
	session_scope,
	context_hash,
	issued_at,
	expires_at,
	status
) values (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
)
