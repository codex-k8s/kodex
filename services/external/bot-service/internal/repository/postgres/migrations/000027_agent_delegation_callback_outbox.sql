-- +goose Up
-- План обязательных callback-публикаций создаётся в той же транзакции, что и
-- callback turn/run. Поля назначения и payload после вставки неизменяемы.
alter table matter_codex_agent_delegations
	add constraint matter_codex_agent_delegations_callback_identity_key
	unique (id, callback_run_id);

create table matter_codex_agent_delegation_callback_deliveries (
	id bigserial primary key,
	delegation_id bigint not null,
	callback_run_id text not null,
	destination text not null,
	publication text not null,
	channel_id text not null,
	root_post_id text not null,
	message text not null,
	props jsonb not null,
	payload_sha256 bytea not null,
	external_id text not null,
	status text not null default 'pending',
	attempt_count integer not null default 0,
	lease_owner text,
	lease_expires_at timestamptz,
	last_attempt_at timestamptz,
	last_error_code text not null default '',
	mattermost_post_id text not null default '',
	delivered_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_agent_delegation_callback_deliveries_callback_identity_fkey
		foreign key (delegation_id, callback_run_id)
		references matter_codex_agent_delegations(id, callback_run_id) on delete restrict,
	constraint matter_codex_agent_delegation_callback_deliveries_identity_key
		unique (delegation_id, callback_run_id, destination, publication),
	constraint matter_codex_agent_delegation_callback_deliveries_external_key unique (external_id),
	constraint matter_codex_agent_delegation_callback_deliveries_callback_run_check
		check (length(trim(callback_run_id)) > 0),
	constraint matter_codex_agent_delegation_callback_deliveries_destination_check
		check (destination in ('source_callback', 'child_return')),
	constraint matter_codex_agent_delegation_callback_deliveries_publication_check
		check (
			(destination = 'source_callback' and publication ~ '^agent_cross_chat_callback:[0-9]{4}$')
			or (destination = 'child_return' and publication ~ '^agent_cross_chat_callback_returned:[0-9]{4}$')
		),
	constraint matter_codex_agent_delegation_callback_deliveries_binding_check
		check (length(trim(channel_id)) > 0 and length(trim(root_post_id)) > 0),
	constraint matter_codex_agent_delegation_callback_deliveries_message_check
		check (length(message) > 0),
	constraint matter_codex_agent_delegation_callback_deliveries_props_check
		check (jsonb_typeof(props) = 'object'),
	constraint matter_codex_agent_delegation_callback_deliveries_props_identity_check
		check (
			props ->> 'matter_codex_event' = split_part(publication, ':', 1)
			and props ->> 'matter_codex_callback_delivery_id' = external_id
			and props ->> 'matter_codex_callback_delegation_id' = delegation_id::text
			and props ->> 'matter_codex_callback_run_id' = callback_run_id
			and props ->> 'matter_codex_callback_destination' = destination
			and props ->> 'matter_codex_callback_publication' = publication
			and props ->> 'matter_codex_callback_payload_sha256' = encode(payload_sha256, 'hex')
			and props - array[
				'matter_codex_event',
				'matter_codex_callback_delivery_id',
				'matter_codex_callback_delegation_id',
				'matter_codex_callback_run_id',
				'matter_codex_callback_destination',
				'matter_codex_callback_publication',
				'matter_codex_callback_payload_sha256'
			] = '{}'::jsonb
		),
	constraint matter_codex_agent_delegation_callback_deliveries_hash_check
		check (octet_length(payload_sha256) = 32),
	constraint matter_codex_agent_delegation_callback_deliveries_external_id_check
		check (external_id ~ '^[a-z0-9]{26}$'),
	constraint matter_codex_agent_delegation_callback_deliveries_status_check
		check (status in ('pending', 'in_flight', 'blocked', 'delivered')),
	constraint matter_codex_agent_delegation_callback_deliveries_attempt_check
		check (attempt_count >= 0),
	constraint matter_codex_agent_delegation_callback_deliveries_lease_check
		check ((status = 'in_flight') = (lease_owner is not null and lease_expires_at is not null)),
	constraint matter_codex_agent_delegation_callback_deliveries_delivered_check
		check ((status = 'delivered') = (length(trim(mattermost_post_id)) > 0 and delivered_at is not null))
);

create index matter_codex_agent_delegation_callback_deliveries_claim_idx
	on matter_codex_agent_delegation_callback_deliveries(delegation_id, callback_run_id, destination, id)
	where status <> 'delivered';

-- +goose StatementBegin
create function matter_codex_guard_agent_delegation_callback_delivery_plan()
returns trigger
language plpgsql
security definer
as $$
begin
	if row(
		new.delegation_id, new.callback_run_id, new.destination, new.publication,
		new.channel_id, new.root_post_id, new.message, new.props,
		new.payload_sha256, new.external_id, new.created_at
	) is distinct from row(
		old.delegation_id, old.callback_run_id, old.destination, old.publication,
		old.channel_id, old.root_post_id, old.message, old.props,
		old.payload_sha256, old.external_id, old.created_at
	) then
		raise exception 'agent delegation callback delivery plan is immutable'
			using errcode = 'check_violation';
	end if;
	if old.status = 'delivered' and new.status <> 'delivered' then
		raise exception 'delivered callback publication is irreversible'
			using errcode = 'check_violation';
	end if;
	return new;
end
$$;
-- +goose StatementEnd

create trigger matter_codex_agent_delegation_callback_delivery_plan_guard
before update on matter_codex_agent_delegation_callback_deliveries
for each row execute function matter_codex_guard_agent_delegation_callback_delivery_plan();

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
begin
	execute format(
		'alter function %I.matter_codex_guard_agent_delegation_callback_delivery_plan() set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'revoke all on table %I.matter_codex_agent_delegation_callback_deliveries from public',
		trusted_schema
	);
	execute format(
		'revoke all on sequence %I.matter_codex_agent_delegation_callback_deliveries_id_seq from public',
		trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_guard_agent_delegation_callback_delivery_plan() from public',
		trusted_schema
	);
	if runtime_role_name is not null then
		execute format(
			'grant select on table %I.matter_codex_agent_delegation_callback_deliveries to %I',
			trusted_schema, runtime_role_name
		);
		execute format(
			'grant insert (delegation_id, callback_run_id, destination, publication, channel_id, root_post_id, message, props, payload_sha256, external_id) on table %I.matter_codex_agent_delegation_callback_deliveries to %I',
			trusted_schema, runtime_role_name
		);
		execute format(
			'grant update (status, attempt_count, lease_owner, lease_expires_at, last_attempt_at, last_error_code, mattermost_post_id, delivered_at, updated_at) on table %I.matter_codex_agent_delegation_callback_deliveries to %I',
			trusted_schema, runtime_role_name
		);
		execute format(
			'grant usage, select on sequence %I.matter_codex_agent_delegation_callback_deliveries_id_seq to %I',
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
	raise exception 'migration 000027 is forward-only: durable callback delivery state cannot be removed safely';
end
$$;
-- +goose StatementEnd
