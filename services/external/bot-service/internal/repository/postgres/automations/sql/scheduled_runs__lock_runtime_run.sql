select
	run.id,
	run.public_id,
	run.occurrence_id,
	run.schedule_id,
	s.public_id,
	s.name,
	run.project_id,
	p.name,
	run.target_agent_role_id,
	role.name,
	run.target_chat_id,
	chat.name,
	run.owner_mattermost_user_id,
	run.owner_mattermost_user_name,
	run.source,
	run.status,
	run.outcome,
	run.safe_summary,
	run.correlation_id,
	run.prompt_version,
	run.callback_contract_version,
	coalesce(run.callback_payload_sha256, ''::bytea),
	coalesce(run.callback_revoked_at, 'epoch'::timestamptz),
	run.callback_expires_at,
	coalesce(run.runtime_session_id, 0),
	run.runtime_session_key,
	coalesce(run.runtime_turn_id, 0),
	run.runtime_run_id,
	run.mattermost_channel_id,
	run.mattermost_root_post_id,
	coalesce(run.started_at, 'epoch'::timestamptz),
	coalesce(run.finished_at, 'epoch'::timestamptz),
	run.created_at,
	run.updated_at
from matter_codex_scheduled_runs run
join matter_codex_automation_schedules s on s.id = run.schedule_id
join matter_codex_projects p on p.id = run.project_id
join matter_codex_agent_roles role
	on role.id = run.target_agent_role_id
	and role.project_id = run.project_id
join matter_codex_chats chat
	on chat.id = run.target_chat_id
	and chat.project_id = run.project_id
where run.project_id = $1
	and run.runtime_run_id = $2
for update of run;
