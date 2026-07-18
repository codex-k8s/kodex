-- +goose Up
-- Манифест фиксирует точное множество строк callback delivery plan. Старые
-- callback run без манифеста не дополняются: новый runtime закрыто откажет им
-- до сетевой доставки, а оператор выполняет отдельное исправление вперёд.
create table matter_codex_agent_delegation_callback_delivery_manifests (
	delegation_id bigint not null,
	callback_run_id text not null,
	expected_count integer not null,
	expected_plan jsonb not null,
	plan_sha256 bytea not null,
	created_at timestamptz not null default now(),
	primary key (delegation_id, callback_run_id),
	constraint matter_codex_agent_delegation_callback_delivery_manifests_callback_fkey
		foreign key (delegation_id, callback_run_id)
		references matter_codex_agent_delegations(id, callback_run_id)
		on delete restrict deferrable initially deferred,
	constraint matter_codex_agent_delegation_callback_delivery_manifests_count_check
		check (expected_count = 2 and jsonb_array_length(expected_plan) = 2),
	constraint matter_codex_agent_delegation_callback_delivery_manifests_plan_check
		check (jsonb_typeof(expected_plan) = 'array'),
	constraint matter_codex_agent_delegation_callback_delivery_manifests_hash_check
		check (octet_length(plan_sha256) = 32)
);

-- +goose StatementBegin
create function matter_codex_agent_delegation_callback_plan_valid(
	p_delegation_id bigint,
	p_callback_run_id text
)
returns boolean
language sql
stable
security definer
as $$
	with actual as (
		select
			count(*)::integer as delivery_count,
			count(distinct delivery.destination)::integer as destination_count,
			bool_or(delivery.destination = 'source_callback') as has_source,
			bool_or(delivery.destination = 'child_return') as has_child,
			coalesce(
				jsonb_agg(
					jsonb_build_object(
						'destination', delivery.destination,
						'publication', delivery.publication,
						'channel_id', delivery.channel_id,
						'root_post_id', delivery.root_post_id,
						'message', delivery.message,
						'props', delivery.props,
						'payload_sha256', encode(delivery.payload_sha256, 'hex'),
						'external_id', delivery.external_id
					)
					order by delivery.destination, delivery.publication
				),
				'[]'::jsonb
			) as delivery_plan
		from matter_codex_agent_delegation_callback_deliveries delivery
		where delivery.delegation_id = p_delegation_id
			and delivery.callback_run_id = p_callback_run_id
	)
	select coalesce((
		select manifest.expected_count = actual.delivery_count
			and actual.delivery_count = 2
			and actual.destination_count = 2
			and actual.has_source
			and actual.has_child
			and manifest.expected_plan = actual.delivery_plan
		from matter_codex_agent_delegation_callback_delivery_manifests manifest
		cross join actual
		where manifest.delegation_id = p_delegation_id
			and manifest.callback_run_id = p_callback_run_id
	), false)
$$;
-- +goose StatementEnd

-- +goose StatementBegin
create function matter_codex_assert_agent_delegation_callback_plan()
returns trigger
language plpgsql
security definer
as $$
declare
	checked_delegation_id bigint;
	checked_callback_run_id text;
begin
	if tg_table_name = 'matter_codex_agent_delegations' then
		if tg_op = 'UPDATE' and new.callback_run_id is not distinct from old.callback_run_id then
			return null;
		end if;
		checked_delegation_id := new.id;
		checked_callback_run_id := new.callback_run_id;
	elsif tg_op = 'DELETE' then
		checked_delegation_id := old.delegation_id;
		checked_callback_run_id := old.callback_run_id;
	else
		checked_delegation_id := new.delegation_id;
		checked_callback_run_id := new.callback_run_id;
	end if;

	if length(trim(coalesce(checked_callback_run_id, ''))) > 0
		and not matter_codex_agent_delegation_callback_plan_valid(
			checked_delegation_id,
			checked_callback_run_id
		)
	then
		raise exception 'agent delegation callback delivery plan is incomplete'
			using errcode = 'check_violation';
	end if;
	return null;
end
$$;
-- +goose StatementEnd

create constraint trigger matter_codex_agent_delegation_callback_plan_delegation_check
after insert or update on matter_codex_agent_delegations
deferrable initially deferred
for each row execute function matter_codex_assert_agent_delegation_callback_plan();

create constraint trigger matter_codex_agent_delegation_callback_plan_delivery_check
after insert or update or delete on matter_codex_agent_delegation_callback_deliveries
deferrable initially deferred
for each row execute function matter_codex_assert_agent_delegation_callback_plan();

create constraint trigger matter_codex_agent_delegation_callback_plan_manifest_check
after insert or update or delete on matter_codex_agent_delegation_callback_delivery_manifests
deferrable initially deferred
for each row execute function matter_codex_assert_agent_delegation_callback_plan();

-- +goose StatementBegin
create function matter_codex_guard_agent_delegation_callback_delivery_manifest()
returns trigger
language plpgsql
security definer
as $$
begin
	if tg_op = 'DELETE' then
		raise exception 'agent delegation callback delivery manifest is immutable'
			using errcode = 'check_violation';
	end if;
	if row(
		new.delegation_id, new.callback_run_id, new.expected_count,
		new.expected_plan, new.plan_sha256, new.created_at
	) is distinct from row(
		old.delegation_id, old.callback_run_id, old.expected_count,
		old.expected_plan, old.plan_sha256, old.created_at
	) then
		raise exception 'agent delegation callback delivery manifest is immutable'
			using errcode = 'check_violation';
	end if;
	return new;
end
$$;
-- +goose StatementEnd

create trigger matter_codex_agent_delegation_callback_delivery_manifest_guard
before update or delete on matter_codex_agent_delegation_callback_delivery_manifests
for each row execute function matter_codex_guard_agent_delegation_callback_delivery_manifest();

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
begin
	execute format(
		'alter function %I.matter_codex_agent_delegation_callback_plan_valid(bigint, text) set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'alter function %I.matter_codex_assert_agent_delegation_callback_plan() set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'alter function %I.matter_codex_guard_agent_delegation_callback_delivery_manifest() set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'revoke all on table %I.matter_codex_agent_delegation_callback_delivery_manifests from public',
		trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_agent_delegation_callback_plan_valid(bigint, text) from public',
		trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_assert_agent_delegation_callback_plan() from public',
		trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_guard_agent_delegation_callback_delivery_manifest() from public',
		trusted_schema
	);
	if runtime_role_name is not null then
		execute format(
			'grant select on table %I.matter_codex_agent_delegation_callback_delivery_manifests to %I',
			trusted_schema, runtime_role_name
		);
		execute format(
			'grant insert (delegation_id, callback_run_id, expected_count, expected_plan, plan_sha256) on table %I.matter_codex_agent_delegation_callback_delivery_manifests to %I',
			trusted_schema, runtime_role_name
		);
		execute format(
			'grant execute on function %I.matter_codex_agent_delegation_callback_plan_valid(bigint, text) to %I',
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
	raise exception 'migration 000028 is forward-only: callback delivery completeness proof cannot be removed safely';
end
$$;
-- +goose StatementEnd
