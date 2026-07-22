-- +goose Up
alter table matter_codex_owner_attention_requests
	add column automation_mattermost_post_create_at bigint,
	add column automation_resolved_by_post_create_at bigint;

-- Открытые binding старого процесса не содержат server-owned CreateAt. Возвращаем их
-- в confirmation-only: новый worker восстановит exact post по delivery id без второго POST.
update matter_codex_owner_attention_requests
set mattermost_post_id = '',
	automation_delivery_claim_token = 'migration-36-confirm-' || id::text,
	automation_delivery_claimed_at = now(),
	automation_delivery_lease_expires_at = now() + interval '1 second',
	automation_delivery_confirmation_pending = true,
	automation_delivery_next_attempt_at = least(automation_delivery_next_attempt_at, now()),
	updated_at = now()
where request_kind = 'automation'
	and status = 'open'
	and mattermost_post_id <> '';

-- Старый процесс мог завершиться после принятого POST, но до записи confirmation-only.
-- Непустой claim является единственным долговечным доказательством возможной сетевой попытки,
-- поэтому такие строки после обновления допускают только сверку Mattermost.
update matter_codex_owner_attention_requests
set automation_delivery_confirmation_pending = true,
	updated_at = now()
where request_kind = 'automation'
	and status = 'open'
	and mattermost_post_id = ''
	and automation_delivery_claim_token is not null;

alter table matter_codex_owner_attention_requests
	add constraint matter_codex_owner_attention_automation_post_ordering_check
		check (
			(
				request_kind = 'generic'
				and automation_mattermost_post_create_at is null
				and automation_resolved_by_post_create_at is null
			)
			or (
				request_kind = 'automation'
				and (
					(
						mattermost_post_id = ''
						and automation_mattermost_post_create_at is null
					)
					or (
						automation_mattermost_post_create_at > 0
						and mattermost_post_id <> ''
					)
				)
				and (
					(
						status = 'open'
						and automation_resolved_by_post_create_at is null
					)
					or (
						status = 'resolved'
						and length(trim(resolved_by_post_id)) > 0
						and automation_mattermost_post_create_at is not null
						and automation_resolved_by_post_create_at > automation_mattermost_post_create_at
					)
				)
			)
		) not valid;

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
begin
	if runtime_role_name is not null then
		execute format(
			'grant update (automation_mattermost_post_create_at, automation_resolved_by_post_create_at) on table %I.matter_codex_owner_attention_requests to %I',
			trusted_schema,
			runtime_role_name
		);
	end if;
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000036 is forward-only: automation delivery ordering proof cannot be removed safely';
end
$$;
-- +goose StatementEnd
