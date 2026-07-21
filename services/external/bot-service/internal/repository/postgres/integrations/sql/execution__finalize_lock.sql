-- name: execution__finalize_lock :one
select state, execution_fence, correlation_id, installation_scope, workspace_scope, session_scope,
	subject_ref, public_id
from matter_codex_tool_invocations
where id = $1
for update;
