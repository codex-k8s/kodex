-- name: catalog__list :many
select distinct capability.capability_key, capability.version
from matter_codex_integration_grants grant_row
join matter_codex_integration_connections connection on connection.id = grant_row.connection_id
join matter_codex_integration_capabilities capability on capability.id = grant_row.capability_id
join matter_codex_agent_sessions session on session.id = $1
join matter_codex_agent_session_turns turn_row on turn_row.id = $3 and turn_row.session_id = session.id
where session.session_key = $2
	and session.project_id = $4
	and session.chat_id = $5
	and session.role_id = $6
	and session.mattermost_channel_id = $7
	and session.mattermost_root_post_id = $8
	and session.token_secret_ref = $9
	and session.status = 'running'
	and session.active_turn_id = turn_row.id
	and turn_row.status = 'running'
	and session.expires_at > $10
	and capability.status = 'active'
	and capability.executor_kind = 'recording_test'
	and capability.approval_required
	and connection.capability_id = capability.id
	and connection.status = 'active'
	and connection.installation_scope = $11
	and connection.workspace_scope = $12
	and grant_row.enabled
	and grant_row.subject_kind = $13
	and grant_row.subject_ref = $14
	and grant_row.installation_scope = $11
	and grant_row.workspace_scope = $12
	and (grant_row.session_scope = '' or grant_row.session_scope = $2)
	and grant_row.valid_from <= $10
	and (grant_row.expires_at is null or grant_row.expires_at > $10)
order by capability.capability_key, capability.version;
