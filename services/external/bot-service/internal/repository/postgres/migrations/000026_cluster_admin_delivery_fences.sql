-- +goose Up
-- Публикация callback удерживает только fence точных session_key. Любой путь отзыва
-- сначала блокирует затронутые fence эксклюзивно, поэтому отсутствие строки revocation
-- также является сериализуемым состоянием без глобальной блокировки таблицы.
create table matter_codex_cluster_admin_delivery_fences (
	session_key text primary key,
	created_at timestamptz not null default now(),
	constraint matter_codex_cluster_admin_delivery_fences_session_key_check
		check (length(trim(session_key)) > 0)
);

insert into matter_codex_cluster_admin_delivery_fences(session_key)
select distinct session_row.session_key
from matter_codex_agent_sessions session_row
order by session_row.session_key
on conflict do nothing;

-- +goose StatementBegin
create function matter_codex_cluster_admin_revocation_session_keys(
	revocation_type text,
	revocation_key text
)
returns table(session_key text)
language sql
stable
security definer
as $$
	with affected_roles(role_id) as (
		select session_row.role_id
		from matter_codex_agent_sessions session_row
		where revocation_type in (
			'agent_role', 'bot_binding', 'runtime_variable_binding',
			'dependency', 'chat_binding'
		)
			and session_row.role_id::text = split_part(revocation_key, ':', 1)
		union
		select subject.subject_key::bigint
		from matter_codex_cluster_admin_subjects subject
		where subject.subject_type = 'agent_role'
			and revocation_type in ('agent_profile', 'profile_dependency')
			and subject.profile_name = case
				when revocation_type = 'agent_profile' then revocation_key
				else split_part(revocation_key, ':', 1)
			end
	), affected_sessions(session_key) as (
		select session_row.session_key
		from matter_codex_agent_sessions session_row
		join affected_roles role on role.role_id = session_row.role_id
		union
		select revocation_key
		where revocation_type = 'session_key'
		union
		select substring(revocation_key from position(':' in revocation_key) + 1)
		where revocation_type = 'session_binding'
			and position(':' in revocation_key) > 0
	)
	select distinct affected.session_key
	from affected_sessions affected
	join matter_codex_agent_sessions session_row
		on session_row.session_key = affected.session_key
	where length(trim(affected.session_key)) > 0
$$;
-- +goose StatementEnd

-- Writer fence является центральным BEFORE-trigger для всех явных INSERT и для
-- dependency-trigger путей. UPDATE/DELETE владельца схемы также участвуют в протоколе.
-- +goose StatementBegin
create function matter_codex_fence_cluster_admin_revocation()
returns trigger
language plpgsql
security definer
as $$
declare
	session_keys text[];
begin
	if tg_op = 'UPDATE' then
		select coalesce(array_agg(distinct affected.session_key order by affected.session_key), array[]::text[])
		into session_keys
		from (
			select session_key
			from matter_codex_cluster_admin_revocation_session_keys(new.resource_type, new.resource_key)
			union
			select session_key
			from matter_codex_cluster_admin_revocation_session_keys(old.resource_type, old.resource_key)
		) affected;
	elsif tg_op = 'DELETE' then
		select coalesce(array_agg(affected.session_key order by affected.session_key), array[]::text[])
		into session_keys
		from matter_codex_cluster_admin_revocation_session_keys(old.resource_type, old.resource_key) affected;
	else
		select coalesce(array_agg(affected.session_key order by affected.session_key), array[]::text[])
		into session_keys
		from matter_codex_cluster_admin_revocation_session_keys(new.resource_type, new.resource_key) affected;
	end if;

	insert into matter_codex_cluster_admin_delivery_fences(session_key)
	select unnest(session_keys)
	on conflict do nothing;

	perform fence.session_key
	from matter_codex_cluster_admin_delivery_fences fence
	where fence.session_key = any(session_keys)
	order by fence.session_key
	for update;

	if tg_op = 'DELETE' then
		return old;
	end if;
	return new;
end
$$;
-- +goose StatementEnd

create trigger matter_codex_cluster_admin_revocation_fence
before insert or update or delete on matter_codex_cluster_admin_revocations
for each row execute function matter_codex_fence_cluster_admin_revocation();

-- Вызывается только внутри транзакции, которая уже взяла session/role/chat/dependency
-- locks в каноническом порядке. Результат должен совпасть с числом точных session_key.
-- +goose StatementBegin
create function matter_codex_lock_cluster_admin_delivery_fences(expected_session_keys text[])
returns integer
language plpgsql
security definer
as $$
declare
	normalized_keys text[];
	locked_count integer;
begin
	select coalesce(array_agg(distinct trim(value) order by trim(value)), array[]::text[])
	into normalized_keys
	from unnest(expected_session_keys) value
	where length(trim(value)) > 0;

	if cardinality(normalized_keys) = 0 then
		raise exception 'cluster-admin delivery fence requires exact session subjects'
			using errcode = 'check_violation';
	end if;

	insert into matter_codex_cluster_admin_delivery_fences(session_key)
	select session_row.session_key
	from matter_codex_agent_sessions session_row
	where session_row.session_key = any(normalized_keys)
	order by session_row.session_key
	on conflict do nothing;

	select count(*) into locked_count
	from (
		select fence.session_key
		from matter_codex_cluster_admin_delivery_fences fence
		where fence.session_key = any(normalized_keys)
		order by fence.session_key
		for share
	) locked;

	return locked_count;
end
$$;
-- +goose StatementEnd

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
begin
	execute format(
		'alter function %I.matter_codex_cluster_admin_revocation_session_keys(text, text) set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'alter function %I.matter_codex_fence_cluster_admin_revocation() set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'alter function %I.matter_codex_lock_cluster_admin_delivery_fences(text[]) set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'revoke all on table %I.matter_codex_cluster_admin_delivery_fences from public',
		trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_cluster_admin_revocation_session_keys(text, text) from public',
		trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_fence_cluster_admin_revocation() from public',
		trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_lock_cluster_admin_delivery_fences(text[]) from public',
		trusted_schema
	);
	if runtime_role_name is not null then
		execute format(
			'grant execute on function %I.matter_codex_lock_cluster_admin_delivery_fences(text[]) to %I',
			trusted_schema, runtime_role_name
		);
	end if;
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000026 is forward-only: scoped delivery fences cannot be relaxed safely';
end
$$;
-- +goose StatementEnd
