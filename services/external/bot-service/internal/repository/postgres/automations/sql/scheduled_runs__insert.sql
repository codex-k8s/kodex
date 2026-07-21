insert into matter_codex_scheduled_runs (
	public_id,
	occurrence_id,
	schedule_id,
	project_id,
	target_agent_role_id,
	target_chat_id,
	owner_mattermost_user_id,
	owner_mattermost_user_name,
	source,
	status,
	correlation_id,
	prompt_version,
	callback_contract_version,
	callback_expires_at
)
values (
	$1, $2, $3, $4, $5, $6, $7, $8,
	'manual', 'queued', $1, $9, $10, $11
)
on conflict (occurrence_id)
do update set occurrence_id = excluded.occurrence_id
returning public_id, (xmax = 0) as created;
