insert into matter_codex_automation_schedules (
	public_id,
	project_id,
	target_agent_role_id,
	target_chat_id,
	name,
	owner_mattermost_user_id,
	owner_mattermost_user_name,
	preset,
	local_time,
	time_zone,
	next_run_at,
	playbook_key,
	prompt_version,
	prompt_snapshot,
	prompt_sha256,
	callback_contract_version,
	creation_idempotency_key,
	command_hash,
	created_at,
	updated_at
)
values (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
	$11, $12, $13, $14, $15, $16, $17, $18, $19, $19
)
on conflict (owner_mattermost_user_id, creation_idempotency_key)
do update set creation_idempotency_key = excluded.creation_idempotency_key
returning public_id, command_hash, (xmax = 0) as created;
