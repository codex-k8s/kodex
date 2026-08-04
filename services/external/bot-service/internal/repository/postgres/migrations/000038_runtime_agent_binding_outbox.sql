-- +goose Up
-- +goose StatementBegin
create function matter_codex_advance_agent_binding_version()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog, pg_temp
as $$
begin
	new.binding_version := old.binding_version + 1;
	return new;
end
$$;
-- +goose StatementEnd

alter table matter_codex_agent_sessions
	add column binding_version bigint not null default 1 check (binding_version > 0);
alter table matter_codex_agent_session_turns
	add column binding_version bigint not null default 1 check (binding_version > 0);

create trigger matter_codex_agent_sessions_binding_version
before update on matter_codex_agent_sessions
for each row execute function matter_codex_advance_agent_binding_version();

create trigger matter_codex_agent_session_turns_binding_version
before update on matter_codex_agent_session_turns
for each row execute function matter_codex_advance_agent_binding_version();

create table matter_codex_runtime_agent_binding_outbox (
	id bigserial primary key,
	idempotency_key text not null unique check (length(idempotency_key) between 16 and 256),
	request_sha256 text not null check (request_sha256 ~ '^[a-f0-9]{64}$'),
	control_session_id text not null check (length(control_session_id) between 1 and 256),
	control_session_version bigint not null check (control_session_version > 0),
	control_turn_id text not null check (length(control_turn_id) between 1 and 256),
	control_turn_version bigint not null check (control_turn_version > 0),
	attempt integer not null check (attempt between 1 and 100),
	input_sha256 text not null check (input_sha256 ~ '^[a-f0-9]{64}$'),
	runtime_revision_id text not null check (length(runtime_revision_id) between 1 and 256),
	runtime_revision_version bigint not null check (runtime_revision_version > 0),
	runtime_revision_sha256 text not null check (runtime_revision_sha256 ~ '^[a-f0-9]{64}$'),
	agent_session_id bigint not null references matter_codex_agent_sessions(id) on delete restrict,
	agent_session_key text not null check (length(agent_session_key) between 1 and 256),
	agent_session_version bigint not null check (agent_session_version > 0),
	agent_session_turn_id bigint not null references matter_codex_agent_session_turns(id) on delete restrict,
	agent_run_id text not null check (length(agent_run_id) between 1 and 256),
	agent_session_turn_version bigint not null check (agent_session_turn_version > 0),
	state text not null default 'PENDING' check (state in ('PENDING', 'LEASED', 'DELIVERED')),
	lease_token text,
	lease_expires_at timestamptz,
	next_attempt_at timestamptz not null default transaction_timestamp(),
	delivery_attempt integer not null default 0 check (delivery_attempt >= 0),
	agent_session_binding_sha256 text check (agent_session_binding_sha256 is null or agent_session_binding_sha256 ~ '^[a-f0-9]{64}$'),
	agent_turn_binding_sha256 text check (agent_turn_binding_sha256 is null or agent_turn_binding_sha256 ~ '^[a-f0-9]{64}$'),
	last_error_code text,
	created_at timestamptz not null default transaction_timestamp(),
	delivered_at timestamptz,
	unique (control_turn_id, attempt),
	unique (agent_session_turn_id, attempt),
	check ((state = 'LEASED') = (lease_token is not null and lease_expires_at is not null)),
	check ((state = 'DELIVERED') = (delivered_at is not null and agent_session_binding_sha256 is not null and agent_turn_binding_sha256 is not null))
);

create index matter_codex_runtime_agent_binding_outbox_claim_idx
	on matter_codex_runtime_agent_binding_outbox (next_attempt_at, id)
	where state in ('PENDING', 'LEASED');

-- +goose StatementBegin
do $$
declare
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
begin
	revoke all on matter_codex_runtime_agent_binding_outbox from public;
	revoke all on sequence matter_codex_runtime_agent_binding_outbox_id_seq from public;
	if runtime_role_name is not null then
		execute format('grant select, insert, update on matter_codex_runtime_agent_binding_outbox to %I', runtime_role_name);
		execute format('grant usage, select on sequence matter_codex_runtime_agent_binding_outbox_id_seq to %I', runtime_role_name);
	end if;
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000038 is forward-only: durable runtime agent binding receipts cannot be removed safely';
end
$$;
-- +goose StatementEnd
