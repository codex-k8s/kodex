-- name: invocation__expire :exec
update matter_codex_tool_invocations
set state = 'expired', reason_code = 'approval.expired', finished_at = $2, updated_at = $2
where id = $1 and state = 'pending';
