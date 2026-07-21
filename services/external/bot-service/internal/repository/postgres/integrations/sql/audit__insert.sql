-- name: audit__insert :exec
insert into matter_codex_audit_events(
	event_type, actor_user_id, actor_user, resource_type, resource_name, summary,
	correlation_id, installation_scope, workspace_scope, session_scope,
	outcome, reason_code, safe_metadata, created_at
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14);
