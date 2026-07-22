-- +goose Up
alter table matter_codex_owner_attention_requests
	add column automation_mattermost_post_create_at bigint,
	add column automation_response_post_create_at bigint,
	add constraint matter_codex_owner_attention_automation_ordering_proof_check
		check (
			(
				request_kind = 'generic'
				and automation_mattermost_post_create_at is null
				and automation_response_post_create_at is null
			)
			or (
				request_kind = 'automation'
				and (
					(
						mattermost_post_id = ''
						and automation_mattermost_post_create_at is null
						and automation_response_post_create_at is null
					)
					or (
						mattermost_post_id <> ''
						and automation_mattermost_post_create_at is not null
						and automation_mattermost_post_create_at > 0
						and (
							(
								status = 'resolved'
								and automation_response_post_create_at is not null
								and automation_response_post_create_at > automation_mattermost_post_create_at
							)
							or (
								status <> 'resolved'
								and automation_response_post_create_at is null
							)
						)
					)
				)
			)
		) not valid;

-- Старые уже привязанные automation-карточки не имеют проверяемого server-owned CreateAt.
-- Ограничение применяется ко всем новым записям и изменениям, а legacy-строки остаются
-- fail-closed для решения до отдельной подтвержденной сверки.

-- +goose StatementBegin
create function matter_codex_guard_automation_human_gate_ordering_proof()
returns trigger
language plpgsql
security definer
as $$
begin
	if old.request_kind = 'automation'
		and old.automation_mattermost_post_create_at is not null
		and row(new.mattermost_post_id, new.automation_mattermost_post_create_at)
			is distinct from row(old.mattermost_post_id, old.automation_mattermost_post_create_at) then
		raise exception 'automation owner attention card ordering proof is immutable'
			using errcode = 'check_violation';
	end if;
	if old.request_kind = 'automation'
		and old.automation_response_post_create_at is not null
		and row(new.resolved_by_post_id, new.automation_response_post_create_at)
			is distinct from row(old.resolved_by_post_id, old.automation_response_post_create_at) then
		raise exception 'automation owner attention response ordering proof is immutable'
			using errcode = 'check_violation';
	end if;
	return new;
end
$$;
-- +goose StatementEnd

create trigger matter_codex_automation_human_gate_ordering_proof_guard
before update of mattermost_post_id, automation_mattermost_post_create_at, resolved_by_post_id, automation_response_post_create_at
on matter_codex_owner_attention_requests
for each row execute function matter_codex_guard_automation_human_gate_ordering_proof();

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
begin
	execute format(
		'alter function %I.matter_codex_guard_automation_human_gate_ordering_proof() set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_guard_automation_human_gate_ordering_proof() from public',
		trusted_schema
	);
	if runtime_role_name is not null then
		execute format('grant update (automation_mattermost_post_create_at, automation_response_post_create_at) on table %I.matter_codex_owner_attention_requests to %I', trusted_schema, runtime_role_name);
	end if;
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000036 is forward-only: automation human gate ordering proofs cannot be removed safely';
end
$$;
-- +goose StatementEnd
