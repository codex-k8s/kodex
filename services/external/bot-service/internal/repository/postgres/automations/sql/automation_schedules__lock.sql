select
	s.id,
	s.public_id,
	s.project_id,
	p.name,
	s.target_agent_role_id,
	r.name,
	s.target_chat_id,
	c.name,
	s.name,
	s.owner_mattermost_user_id,
	s.owner_mattermost_user_name,
	s.preset,
	s.local_time,
	s.time_zone,
	s.enabled,
	s.next_run_at,
	s.playbook_key,
	s.prompt_version,
	s.prompt_snapshot,
	s.prompt_sha256,
	s.callback_contract_version,
	s.command_hash,
	s.created_at,
	s.updated_at
from matter_codex_automation_schedules s
join matter_codex_projects p on p.id = s.project_id
join matter_codex_agent_roles r
	on r.id = s.target_agent_role_id
	and r.project_id = s.project_id
join matter_codex_chats c
	on c.id = s.target_chat_id
	and c.project_id = s.project_id
where s.public_id = $1
for update of s;
