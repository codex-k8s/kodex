-- name: execution__claim_select :one
select
	invocation.id, invocation.public_id, invocation.state, invocation.execution_fence,
	invocation.arguments ->> 'namespace', invocation.arguments ->> 'workload_kind', invocation.arguments ->> 'workload_name',
	encode(invocation.arguments_sha256, 'hex')
from matter_codex_tool_invocations invocation
join matter_codex_approval_requests approval on approval.invocation_id = invocation.id and approval.state = 'approved'
where invocation.state = 'approved'
	or (invocation.state = 'executing' and invocation.execution_lease_expires_at <= $1)
order by invocation.id
limit 1
for update of invocation skip locked;
