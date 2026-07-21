-- name: invocation__by_id :one
select
	invocation.id, invocation.public_id, invocation.state, invocation.reason_code,
	invocation.arguments ->> 'namespace', invocation.arguments ->> 'workload_kind', invocation.arguments ->> 'workload_name',
	encode(invocation.arguments_sha256, 'hex'), encode(invocation.approval_binding_sha256, 'hex'), invocation.correlation_id,
	approval.id, approval.public_id, approval.mattermost_post_id,
	coalesce(execution.execution_id, ''),
	coalesce(execution.result ->> 'namespace', ''),
	coalesce(execution.result ->> 'workload_kind', ''),
	coalesce(execution.result ->> 'workload_name', ''),
	coalesce(execution.result ->> 'recorded_at', '')
from matter_codex_tool_invocations invocation
join matter_codex_approval_requests approval on approval.invocation_id = invocation.id
left join matter_codex_integration_test_executions execution on execution.invocation_id = invocation.id
where invocation.id = $1;
