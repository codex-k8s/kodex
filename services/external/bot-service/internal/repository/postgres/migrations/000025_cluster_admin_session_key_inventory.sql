-- +goose Up
-- Frozen session_key является глобальным монотонным идентификатором. Текущая роль строки
-- не может изменить классификацию уже зафиксированного или отозванного ключа.
create unique index matter_codex_cluster_admin_session_bindings_session_key_uq
	on matter_codex_cluster_admin_session_bindings (session_key);

insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason, revoked_at)
select 'session_key', binding.session_key, revocation.reason, revocation.revoked_at
from matter_codex_cluster_admin_session_bindings binding
join matter_codex_cluster_admin_revocations revocation
	on revocation.resource_type = 'session_binding'
	and revocation.resource_key = binding.role_id::text || ':' || binding.session_key
on conflict do nothing;

-- +goose StatementBegin
create or replace function matter_codex_guard_cluster_admin_session()
returns trigger
language plpgsql
as $$
declare
	frozen_binding matter_codex_cluster_admin_session_bindings%rowtype;
	frozen_state jsonb;
	key_value text;
	legacy_key_value text;
begin
	if tg_op = 'DELETE' then
		key_value := old.session_key;
	else
		key_value := new.session_key;
	end if;

	select binding.* into frozen_binding
	from matter_codex_cluster_admin_session_bindings binding
	where binding.session_key = key_value;
	frozen_state := frozen_binding.privilege_state;

	if tg_op = 'DELETE' then
		if frozen_state is not null then
			legacy_key_value := frozen_binding.role_id::text || ':' || key_value;
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('session_binding', legacy_key_value, 'session deleted') on conflict do nothing;
			insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
			values ('session_key', key_value, 'session deleted') on conflict do nothing;
		end if;
		return old;
	end if;

	if tg_op = 'UPDATE' and old.session_key is distinct from new.session_key and exists (
		select 1
		from matter_codex_cluster_admin_session_bindings binding
		where binding.session_key = old.session_key
	) then
		perform matter_codex_cluster_admin_record_denied('session_key', old.session_key, 'session.key_change');
		return null;
	end if;

	if tg_op = 'UPDATE'
		and old.status in ('blocked', 'closed')
		and new.status is distinct from old.status
	then
		perform matter_codex_cluster_admin_record_denied('session_key', key_value, 'session.reenable');
		return null;
	end if;

	if tg_op = 'UPDATE'
		and old.status not in ('blocked', 'closed')
		and new.status in ('blocked', 'closed')
		and frozen_state is not null
	then
		legacy_key_value := frozen_binding.role_id::text || ':' || key_value;
		insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
		values ('session_binding', legacy_key_value, 'session ' || new.status) on conflict do nothing;
		insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
		values ('session_key', key_value, 'session ' || new.status) on conflict do nothing;
		return new;
	end if;

	legacy_key_value := coalesce(frozen_binding.role_id, new.role_id)::text || ':' || key_value;
	if exists (
		select 1
		from matter_codex_cluster_admin_revocations revocation
		where (revocation.resource_type = 'session_key' and revocation.resource_key = key_value)
			or (revocation.resource_type = 'session_binding' and revocation.resource_key = legacy_key_value)
	) then
		perform matter_codex_cluster_admin_record_denied('session_key', key_value, 'session.revoked_key_reuse');
		return null;
	end if;

	if frozen_state is not null and (
		frozen_binding.role_id <> new.role_id
		or frozen_binding.project_id <> new.project_id
		or frozen_binding.chat_id <> new.chat_id
		or frozen_binding.mattermost_channel_id <> new.mattermost_channel_id
		or frozen_state <> matter_codex_cluster_admin_session_state(new)
		or not matter_codex_cluster_admin_binding_exact(new.role_id, new.chat_id)
	) then
		perform matter_codex_cluster_admin_record_denied('session_key', key_value, 'session.frozen_key_rebind');
		return null;
	end if;

	if frozen_state is null and exists (
		select 1 from matter_codex_cluster_admin_subjects subject
		where subject.subject_type = 'agent_role' and subject.subject_key = new.role_id::text
	) then
		perform matter_codex_cluster_admin_record_denied('session_key', key_value, 'session.unfrozen_admin');
		return null;
	end if;

	return new;
end
$$;
-- +goose StatementEnd

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
begin
	execute format(
		'alter function %I.matter_codex_guard_cluster_admin_session() set search_path = pg_catalog, %I, pg_temp',
		trusted_schema,
		trusted_schema
	);
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000025 is forward-only: frozen session_key inventory cannot be relaxed safely';
end
$$;
-- +goose StatementEnd
