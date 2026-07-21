-- name: approval_delivery__get :one
select
	approval.id, approval.public_id, invocation.public_id, capability.capability_key, connection.public_id,
	invocation.arguments ->> 'namespace', invocation.arguments ->> 'workload_kind', invocation.arguments ->> 'workload_name',
	encode(invocation.arguments_sha256, 'hex'), encode(invocation.approval_binding_sha256, 'hex'),
	approval.risk_class, approval.exact_approver_user_id, approval.exact_approver_user_name,
	invocation.workspace_scope, invocation.session_scope,
	approval.mattermost_channel_id, approval.mattermost_root_post_id, approval.mattermost_post_id,
	approval.expires_at, approval.delivery_lease_owner
from matter_codex_approval_requests approval
join matter_codex_tool_invocations invocation on invocation.id = approval.invocation_id
join matter_codex_integration_capabilities capability on capability.id = invocation.capability_id
join matter_codex_integration_connections connection on connection.id = invocation.connection_id
where approval.id = $1 and approval.delivery_state = 'in_flight' and approval.delivery_lease_owner = $2;
