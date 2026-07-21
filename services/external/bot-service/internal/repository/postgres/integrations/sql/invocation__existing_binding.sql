-- name: invocation__existing_binding :one
select
	id, encode(arguments_sha256, 'hex'), encode(approval_binding_sha256, 'hex'),
	capability_id, capability_revision, connection_id, connection_revision,
	grant_id, grant_revision, subject_kind, subject_ref,
	installation_scope, workspace_scope, session_scope
from matter_codex_tool_invocations
where session_id = $1 and capability_id = $2 and idempotency_key = $3
for key share;
