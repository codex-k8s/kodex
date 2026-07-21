-- name: execution__cancel :exec
update matter_codex_tool_invocations
set state = 'cancelled', reason_code = $3, execution_lease_owner = '', execution_lease_expires_at = null,
	finished_at = $4, updated_at = $4
where id = $1 and state = 'executing' and execution_fence = $2 and execution_lease_owner = $5;
