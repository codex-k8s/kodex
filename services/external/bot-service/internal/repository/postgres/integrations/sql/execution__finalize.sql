-- name: execution__finalize :exec
update matter_codex_tool_invocations
set state = 'succeeded', result = $3::jsonb, reason_code = '',
	execution_lease_owner = '', execution_lease_expires_at = null, finished_at = $4, updated_at = $4
where id = $1 and state = 'executing' and execution_fence = $2;
