-- name: interaction_capabilities__consume_pending :one
update matter_codex_interaction_capabilities
set status = 'consumed', consumed_at = $11
where token_hash = $1
	and kind = $2
	and operation = $3
	and resource_type = $4
	and resource_id = $5
	and channel_id = $6
	and post_binding = $7
	and actor_user_id = $8
	and context_hash = $9
	and status = 'pending'
	and expires_at > $10
returning kind, operation, resource_type, resource_id, channel_id, post_binding,
	actor_user_id, actor_user_name, installation_scope, workspace_scope, session_scope,
	issued_at, expires_at, consumed_at
