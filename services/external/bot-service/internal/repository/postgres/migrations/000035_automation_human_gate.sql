-- +goose Up
alter table matter_codex_schedule_occurrences
	drop constraint matter_codex_schedule_occurrences_status_check;

alter table matter_codex_schedule_occurrences
	add constraint matter_codex_schedule_occurrences_status_check
		check (status in ('queued', 'running', 'waiting_owner', 'succeeded', 'failed'));

alter table matter_codex_scheduled_runs
	drop constraint matter_codex_scheduled_runs_status_check,
	drop constraint matter_codex_scheduled_runs_terminal_check;

alter table matter_codex_scheduled_runs
	add constraint matter_codex_scheduled_runs_status_check
		check (status in ('queued', 'running', 'waiting_owner', 'succeeded', 'failed')),
	add constraint matter_codex_scheduled_runs_terminal_check
		check (
			(status in ('queued', 'running') and outcome = '' and finished_at is null)
			or (
				status = 'waiting_owner'
				and outcome = 'requires_human'
				and callback_payload_sha256 is not null
				and finished_at is null
			)
			or (status in ('succeeded', 'failed') and outcome <> '' and finished_at is not null)
		),
	add constraint matter_codex_scheduled_runs_owner_gate_reference_key
		unique (id, runtime_turn_id, project_id);

alter table matter_codex_process_turns
	add constraint matter_codex_process_turns_run_turn_key
		unique (process_run_id, turn_id);

alter table matter_codex_process_runs
	add constraint matter_codex_process_runs_automation_context_key
		unique (id, project_id, policy_revision_id, root_initiator_user_id);

alter table matter_codex_owner_attention_requests
	add column request_kind text not null default 'generic',
	add column automation_scheduled_run_id bigint,
	add column automation_project_id bigint,
	add column automation_policy_revision_id bigint,
	add column automation_root_initiator_user_id text,
	add column automation_mattermost_channel_id text,
	add column automation_mattermost_root_post_id text,
	add column automation_delivery_id text,
	add column automation_delivery_message text,
	add column automation_delivery_props jsonb,
	add column automation_delivery_payload_sha256 bytea,
	add column automation_delivery_claim_token text,
	add column automation_delivery_claimed_at timestamptz,
	add column automation_delivery_lease_expires_at timestamptz,
	add column automation_delivery_confirmation_pending boolean not null default false,
	add column automation_delivery_next_attempt_at timestamptz not null default '-infinity',
	add column automation_delivery_fence bigint not null default 0,
	add constraint matter_codex_owner_attention_request_kind_check
		check (request_kind in ('generic', 'automation')),
	add constraint matter_codex_owner_attention_automation_run_key
		unique (automation_scheduled_run_id),
	add constraint matter_codex_owner_attention_automation_delivery_key
		unique (automation_delivery_id),
	add constraint matter_codex_owner_attention_automation_shape_check
		check (
			(
				request_kind = 'generic'
				and automation_scheduled_run_id is null
				and automation_project_id is null
				and automation_policy_revision_id is null
				and automation_root_initiator_user_id is null
				and automation_mattermost_channel_id is null
				and automation_mattermost_root_post_id is null
				and automation_delivery_id is null
				and automation_delivery_message is null
				and automation_delivery_props is null
				and automation_delivery_payload_sha256 is null
				and automation_delivery_claim_token is null
				and automation_delivery_claimed_at is null
				and automation_delivery_lease_expires_at is null
				and not automation_delivery_confirmation_pending
				and automation_delivery_fence = 0
			)
			or (
				request_kind = 'automation'
				and automation_scheduled_run_id is not null
				and automation_project_id is not null
				and automation_policy_revision_id is not null
				and length(trim(automation_root_initiator_user_id)) > 0
				and length(trim(automation_mattermost_channel_id)) > 0
				and length(trim(automation_mattermost_root_post_id)) > 0
				and automation_delivery_id ~ '^[a-z0-9]{26}$'
				and length(automation_delivery_message) > 0
				and position(E'\n\n#notrigger' in automation_delivery_message) > 0
				and jsonb_typeof(automation_delivery_props) = 'object'
				and automation_delivery_props->>'matter_codex_event' = 'automation_owner_attention'
				and automation_delivery_props->>'matter_codex_callback_delivery_id' = automation_delivery_id
				and automation_delivery_props->>'matter_codex_automation_run_id' <> ''
				and automation_delivery_props->>'matter_codex_human_decision_status' = 'pending'
				and octet_length(automation_delivery_payload_sha256) = 32
				and (
					(
						automation_delivery_claim_token is null
						and automation_delivery_claimed_at is null
						and automation_delivery_lease_expires_at is null
					)
					or (
						length(trim(automation_delivery_claim_token)) between 16 and 128
						and automation_delivery_claimed_at is not null
						and automation_delivery_lease_expires_at > automation_delivery_claimed_at
					)
				)
				and (
					not automation_delivery_confirmation_pending
					or (mattermost_post_id = '' and automation_delivery_claim_token is not null)
				)
				and automation_delivery_fence >= 0
			)
		),
	add constraint matter_codex_owner_attention_automation_run_fk
		foreign key (automation_scheduled_run_id, turn_id, automation_project_id)
		references matter_codex_scheduled_runs(id, runtime_turn_id, project_id)
		on delete restrict,
	add constraint matter_codex_owner_attention_automation_process_turn_fk
		foreign key (process_run_id, turn_id)
		references matter_codex_process_turns(process_run_id, turn_id)
		on delete cascade not valid,
	add constraint matter_codex_owner_attention_automation_process_fk
		foreign key (
			process_run_id,
			automation_project_id,
			automation_policy_revision_id,
			automation_root_initiator_user_id
		)
		references matter_codex_process_runs(
			id,
			project_id,
			policy_revision_id,
			root_initiator_user_id
		)
		on delete cascade;

-- Старую общую уникальность заменяют независимые namespace generic и automation.
-- +goose StatementBegin
do $$
declare
	unique_constraint_name text;
begin
	select candidate.conname
	into unique_constraint_name
	from pg_constraint candidate
	where candidate.conrelid = 'matter_codex_owner_attention_requests'::regclass
		and candidate.contype = 'u'
		and pg_get_constraintdef(candidate.oid) = 'UNIQUE (process_run_id, idempotency_key)';
	if unique_constraint_name is null then
		raise exception 'generic owner attention uniqueness constraint was not found';
	end if;
	execute format('alter table matter_codex_owner_attention_requests drop constraint %I', unique_constraint_name);
end
$$;
-- +goose StatementEnd

create unique index matter_codex_owner_attention_generic_idempotency_key
	on matter_codex_owner_attention_requests(process_run_id, idempotency_key)
	where request_kind = 'generic';

create index matter_codex_owner_attention_automation_pending_idx
	on matter_codex_owner_attention_requests(automation_delivery_next_attempt_at, id)
	where request_kind = 'automation'
		and status = 'open'
		and mattermost_post_id = '';

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
begin
	if runtime_role_name is not null then
		execute format('grant select on table %I.matter_codex_owner_attention_requests, %I.matter_codex_process_runs, %I.matter_codex_process_turns to %I', trusted_schema, trusted_schema, trusted_schema, runtime_role_name);
		execute format('grant insert on table %I.matter_codex_owner_attention_requests to %I', trusted_schema, runtime_role_name);
		execute format('grant update (status, mattermost_post_id, resolved_at, resolved_by_user_id, resolved_by_post_id, updated_at, automation_delivery_claim_token, automation_delivery_claimed_at, automation_delivery_lease_expires_at, automation_delivery_confirmation_pending, automation_delivery_next_attempt_at, automation_delivery_fence) on table %I.matter_codex_owner_attention_requests to %I', trusted_schema, runtime_role_name);
		execute format('grant update (status, updated_at, finished_at) on table %I.matter_codex_process_runs to %I', trusted_schema, runtime_role_name);
	end if;
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000035 is forward-only: automation human gates cannot be removed safely';
end
$$;
-- +goose StatementEnd
