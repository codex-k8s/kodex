-- name: invocation__insert :one
insert into matter_codex_tool_invocations(
	public_id, session_id, turn_id, project_id, chat_id, role_id,
	subject_kind, subject_ref, installation_scope, workspace_scope, session_scope, session_token_secret_ref,
	capability_id, capability_revision, connection_id, connection_revision, grant_id, grant_revision,
	idempotency_key, arguments, arguments_sha256, approval_binding_sha256,
	correlation_id, state, created_at, updated_at
)
values (
	$1, $2, $3, $4, $5, $6,
	$7, $8, $9, $10, $11, $12,
	$13, $14, $15, $16, $17, $18,
	$19, $20::jsonb, $21, $22,
	$23, 'pending', $24, $24
)
returning id;
