-- name: approval__lock_by_invocation :one
select
	approval.id, approval.public_id, approval.invocation_id, approval.state, encode(approval.approval_binding_sha256, 'hex'),
	approval.exact_approver_user_id, approval.exact_approver_user_name, approval.expires_at,
	approval.mattermost_channel_id, approval.mattermost_root_post_id, approval.mattermost_post_id,
	invocation.state, invocation.correlation_id, invocation.installation_scope,
	invocation.workspace_scope, invocation.session_scope
from matter_codex_approval_requests approval
join matter_codex_tool_invocations invocation on invocation.id = approval.invocation_id
where approval.invocation_id = $1
for update of approval, invocation;
