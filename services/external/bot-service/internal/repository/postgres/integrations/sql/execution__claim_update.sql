-- name: execution__claim_update :exec
update matter_codex_tool_invocations
set state = 'executing', execution_fence = $2, execution_lease_owner = $3,
	execution_lease_expires_at = $4, executing_at = coalesce(executing_at, $1), updated_at = $1
where id = $5 and state in ('approved', 'executing');
