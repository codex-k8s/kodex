-- name: binding__authorize :one
select
	capability.id, capability.public_id, capability.capability_key, capability.version, capability.revision,
	connection.id, connection.public_id, connection.revision,
	grant_row.id, grant_row.public_id, grant_row.revision
from matter_codex_integration_connections connection
join matter_codex_integration_capabilities capability on capability.id = connection.capability_id
join matter_codex_integration_grants grant_row
	on grant_row.connection_id = connection.id and grant_row.capability_id = capability.id
join matter_codex_agent_sessions session on session.id = $1
join matter_codex_agent_session_turns turn_row on turn_row.id = session.active_turn_id and turn_row.session_id = session.id
where session.session_key = $2
	and session.project_id = $3
	and session.chat_id = $4
	and session.role_id = $5
	and session.mattermost_channel_id = $6
	and session.mattermost_root_post_id = $7
	and session.token_secret_ref = $8
	and session.status = 'running'
	and session.active_turn_id = $9
	and turn_row.status = 'running'
	and session.expires_at > $10
	and connection.public_id = $11
	and connection.status = 'active'
	and connection.installation_scope = $12
	and connection.workspace_scope = $13
	and capability.capability_key = $14
	and capability.version = 1
	and capability.status = 'active'
	and capability.approval_required
	and capability.executor_kind = 'recording_test'
	and grant_row.enabled
	and grant_row.subject_kind = $15
	and grant_row.subject_ref = $16
	and grant_row.installation_scope = $12
	and grant_row.workspace_scope = $13
	and (grant_row.session_scope = '' or grant_row.session_scope = $2)
	and grant_row.allowed_namespace = $17
	and grant_row.allowed_workload_kind = $18
	and grant_row.allowed_workload_name = $19
	and grant_row.valid_from <= $10
	and (grant_row.expires_at is null or grant_row.expires_at > $10)
order by (grant_row.session_scope = $2) desc, grant_row.id
limit 1
for key share of connection, capability, grant_row, session, turn_row;
