-- name: execution__authorize :one
select invocation.id
from matter_codex_tool_invocations invocation
join matter_codex_approval_requests approval on approval.invocation_id = invocation.id
join matter_codex_agent_sessions session on session.id = invocation.session_id
join matter_codex_integration_capabilities capability on capability.id = invocation.capability_id
join matter_codex_integration_connections connection on connection.id = invocation.connection_id
join matter_codex_integration_grants grant_row on grant_row.id = invocation.grant_id
where invocation.id = $1
	and invocation.state = 'executing'
	and invocation.execution_fence = $2
	and invocation.execution_lease_owner = $3
	and invocation.execution_lease_expires_at > $4
	and approval.state = 'approved'
	and approval.approval_binding_sha256 = invocation.approval_binding_sha256
	and session.id = invocation.session_id
	and session.session_key = invocation.session_scope
	and session.project_id = invocation.project_id
	and session.chat_id = invocation.chat_id
	and session.role_id = invocation.role_id
	and session.token_secret_ref = invocation.session_token_secret_ref
	and session.mattermost_channel_id = approval.mattermost_channel_id
	and session.mattermost_root_post_id = approval.mattermost_root_post_id
	and session.status in ('idle', 'running')
	and session.expires_at > $4
	and capability.id = invocation.capability_id
	and capability.revision = invocation.capability_revision
	and capability.status = 'active'
	and capability.executor_kind = 'recording_test'
	and capability.approval_required
	and connection.id = invocation.connection_id
	and connection.capability_id = invocation.capability_id
	and connection.revision = invocation.connection_revision
	and connection.status = 'active'
	and connection.installation_scope = invocation.installation_scope
	and connection.workspace_scope = invocation.workspace_scope
	and connection.credential_ref = ''
	and grant_row.id = invocation.grant_id
	and grant_row.connection_id = invocation.connection_id
	and grant_row.capability_id = invocation.capability_id
	and grant_row.revision = invocation.grant_revision
	and grant_row.enabled
	and grant_row.subject_kind = invocation.subject_kind
	and grant_row.subject_ref = invocation.subject_ref
	and grant_row.installation_scope = invocation.installation_scope
	and grant_row.workspace_scope = invocation.workspace_scope
	and (grant_row.session_scope = '' or grant_row.session_scope = invocation.session_scope)
	and grant_row.allowed_namespace = invocation.arguments ->> 'namespace'
	and grant_row.allowed_workload_kind = invocation.arguments ->> 'workload_kind'
	and grant_row.allowed_workload_name = invocation.arguments ->> 'workload_name'
	and grant_row.valid_from <= $4
	and (grant_row.expires_at is null or grant_row.expires_at > $4)
for update of invocation
for key share of approval, session, capability, connection, grant_row;
