-- +goose Up
create table matter_codex_automation_schedules (
	id bigserial primary key,
	public_id text not null unique,
	project_id bigint not null references matter_codex_projects(id) on delete restrict,
	target_agent_role_id bigint not null references matter_codex_agent_roles(id) on delete restrict,
	target_chat_id bigint not null references matter_codex_chats(id) on delete restrict,
	name text not null,
	owner_mattermost_user_id text not null,
	owner_mattermost_user_name text not null default '',
	preset text not null,
	local_time text not null,
	time_zone text not null,
	enabled boolean not null default true,
	next_run_at timestamptz not null,
	playbook_key text not null,
	prompt_version text not null,
	prompt_snapshot text not null,
	prompt_sha256 bytea not null,
	callback_contract_version text not null,
	creation_idempotency_key text not null,
	command_hash bytea not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_automation_schedules_public_id_check
		check (public_id ~ '^schedule-[a-f0-9]{32}$'),
	constraint matter_codex_automation_schedules_owner_check
		check (length(trim(owner_mattermost_user_id)) > 0),
	constraint matter_codex_automation_schedules_preset_check
		check (preset = 'daily'),
	constraint matter_codex_automation_schedules_local_time_check
		check (local_time ~ '^(0[0-9]|1[0-9]|2[0-3]):[0-5][0-9]$'),
	constraint matter_codex_automation_schedules_prompt_hash_check
		check (octet_length(prompt_sha256) = 32),
	constraint matter_codex_automation_schedules_command_hash_check
		check (octet_length(command_hash) = 32),
	constraint matter_codex_automation_schedules_creation_key
		unique (owner_mattermost_user_id, creation_idempotency_key)
);

create index matter_codex_automation_schedules_owner_project_idx
	on matter_codex_automation_schedules(owner_mattermost_user_id, project_id, created_at desc);

create index matter_codex_automation_schedules_due_idx
	on matter_codex_automation_schedules(next_run_at, id)
	where enabled;

create table matter_codex_schedule_occurrences (
	id bigserial primary key,
	public_id text not null unique,
	schedule_id bigint not null references matter_codex_automation_schedules(id) on delete restrict,
	project_id bigint not null references matter_codex_projects(id) on delete restrict,
	source text not null,
	idempotency_key text not null,
	scheduled_for timestamptz not null,
	status text not null default 'queued',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_schedule_occurrences_public_id_check
		check (public_id ~ '^occurrence-[a-f0-9]{32}$'),
	constraint matter_codex_schedule_occurrences_source_check
		check (source in ('manual', 'scheduled')),
	constraint matter_codex_schedule_occurrences_status_check
		check (status in ('queued', 'running', 'succeeded', 'failed')),
	constraint matter_codex_schedule_occurrences_command_key
		unique (schedule_id, idempotency_key)
);

create index matter_codex_schedule_occurrences_project_idx
	on matter_codex_schedule_occurrences(project_id, created_at desc);

create unique index matter_codex_schedule_occurrences_scheduled_key
	on matter_codex_schedule_occurrences(schedule_id, scheduled_for)
	where source = 'scheduled';

create table matter_codex_scheduled_runs (
	id bigserial primary key,
	public_id text not null unique,
	occurrence_id bigint not null unique references matter_codex_schedule_occurrences(id) on delete restrict,
	schedule_id bigint not null references matter_codex_automation_schedules(id) on delete restrict,
	project_id bigint not null references matter_codex_projects(id) on delete restrict,
	target_agent_role_id bigint not null references matter_codex_agent_roles(id) on delete restrict,
	target_chat_id bigint not null references matter_codex_chats(id) on delete restrict,
	owner_mattermost_user_id text not null,
	owner_mattermost_user_name text not null default '',
	source text not null,
	status text not null default 'queued',
	outcome text not null default '',
	safe_summary text not null default '',
	correlation_id text not null,
	prompt_version text not null,
	callback_contract_version text not null,
	callback_payload_sha256 bytea,
	callback_revoked_at timestamptz,
	callback_expires_at timestamptz not null,
	runtime_session_id bigint references matter_codex_agent_sessions(id) on delete restrict,
	runtime_session_key text not null default '',
	runtime_turn_id bigint references matter_codex_agent_session_turns(id) on delete restrict,
	runtime_run_id text not null default '',
	mattermost_channel_id text not null default '',
	mattermost_root_post_id text not null default '',
	started_at timestamptz,
	finished_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_scheduled_runs_public_id_check
		check (public_id ~ '^scheduled-run-[a-f0-9]{32}$'),
	constraint matter_codex_scheduled_runs_source_check
		check (source in ('manual', 'scheduled')),
	constraint matter_codex_scheduled_runs_status_check
		check (status in ('queued', 'running', 'succeeded', 'failed')),
	constraint matter_codex_scheduled_runs_outcome_check
		check (outcome in ('', 'no_action', 'action_taken', 'requires_human', 'failed')),
	constraint matter_codex_scheduled_runs_callback_hash_check
		check (callback_payload_sha256 is null or octet_length(callback_payload_sha256) = 32),
	constraint matter_codex_scheduled_runs_terminal_check
		check (
			(status in ('queued', 'running') and outcome = '' and finished_at is null)
			or (status in ('succeeded', 'failed') and outcome <> '' and finished_at is not null)
		),
	constraint matter_codex_scheduled_runs_runtime_binding_check
		check (
			(runtime_session_id is null and runtime_turn_id is null and runtime_session_key = '' and runtime_run_id = '')
			or (runtime_session_id is not null and runtime_turn_id is not null and runtime_session_key <> '' and runtime_run_id <> '')
		)
);

create index matter_codex_scheduled_runs_history_idx
	on matter_codex_scheduled_runs(owner_mattermost_user_id, project_id, schedule_id, created_at desc);

create index matter_codex_scheduled_runs_callback_idx
	on matter_codex_scheduled_runs(public_id, project_id, runtime_session_id, runtime_turn_id, runtime_run_id);

create table matter_codex_automation_audit_events (
	id bigserial primary key,
	project_id bigint not null references matter_codex_projects(id) on delete restrict,
	schedule_id bigint references matter_codex_automation_schedules(id) on delete restrict,
	scheduled_run_id bigint references matter_codex_scheduled_runs(id) on delete restrict,
	event_type text not null,
	actor_user_id text not null default '',
	actor_user_name text not null default '',
	correlation_id text not null,
	safe_summary text not null default '',
	created_at timestamptz not null default now(),
	constraint matter_codex_automation_audit_events_terminal_key
		unique nulls not distinct (schedule_id, scheduled_run_id, event_type)
);

create index matter_codex_automation_audit_events_project_idx
	on matter_codex_automation_audit_events(project_id, created_at desc);

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
	table_name text;
	sequence_name text;
begin
	for table_name in select unnest(array[
		'matter_codex_automation_schedules',
		'matter_codex_schedule_occurrences',
		'matter_codex_scheduled_runs',
		'matter_codex_automation_audit_events'
	]) loop
		execute format('revoke all on table %I.%I from public', trusted_schema, table_name);
	end loop;
	for sequence_name in select unnest(array[
		'matter_codex_automation_schedules_id_seq',
		'matter_codex_schedule_occurrences_id_seq',
		'matter_codex_scheduled_runs_id_seq',
		'matter_codex_automation_audit_events_id_seq'
	]) loop
		execute format('revoke all on sequence %I.%I from public', trusted_schema, sequence_name);
	end loop;
	if runtime_role_name is not null then
		execute format('grant select on table %I.matter_codex_automation_schedules, %I.matter_codex_schedule_occurrences, %I.matter_codex_scheduled_runs, %I.matter_codex_automation_audit_events to %I', trusted_schema, trusted_schema, trusted_schema, trusted_schema, runtime_role_name);
		execute format('grant insert on table %I.matter_codex_automation_schedules, %I.matter_codex_schedule_occurrences, %I.matter_codex_scheduled_runs, %I.matter_codex_automation_audit_events to %I', trusted_schema, trusted_schema, trusted_schema, trusted_schema, runtime_role_name);
		execute format('grant update (creation_idempotency_key) on table %I.matter_codex_automation_schedules to %I', trusted_schema, runtime_role_name);
		execute format('grant update (idempotency_key, status, updated_at) on table %I.matter_codex_schedule_occurrences to %I', trusted_schema, runtime_role_name);
		execute format('grant update (occurrence_id, status, outcome, safe_summary, callback_payload_sha256, callback_revoked_at, runtime_session_id, runtime_session_key, runtime_turn_id, runtime_run_id, mattermost_channel_id, mattermost_root_post_id, started_at, finished_at, updated_at) on table %I.matter_codex_scheduled_runs to %I', trusted_schema, runtime_role_name);
		for sequence_name in select unnest(array[
			'matter_codex_automation_schedules_id_seq',
			'matter_codex_schedule_occurrences_id_seq',
			'matter_codex_scheduled_runs_id_seq',
			'matter_codex_automation_audit_events_id_seq'
		]) loop
			execute format('grant usage, select on sequence %I.%I to %I', trusted_schema, sequence_name, runtime_role_name);
		end loop;
	end if;
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000030 is forward-only: automation schedules and history cannot be removed safely';
end
$$;
-- +goose StatementEnd
