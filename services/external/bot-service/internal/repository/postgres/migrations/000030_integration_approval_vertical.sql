-- +goose Up
alter table matter_codex_audit_events
	add column if not exists correlation_id text not null default '',
	add column if not exists installation_scope text not null default '',
	add column if not exists workspace_scope text not null default '',
	add column if not exists session_scope text not null default '',
	add column if not exists outcome text not null default '',
	add column if not exists reason_code text not null default '',
	add column if not exists safe_metadata jsonb not null default '{}'::jsonb;

alter table matter_codex_audit_events
	add constraint matter_codex_audit_events_safe_metadata_check
	check (jsonb_typeof(safe_metadata) = 'object');

create index matter_codex_audit_events_correlation_idx
	on matter_codex_audit_events(correlation_id, id)
	where correlation_id <> '';

create table matter_codex_integration_capabilities (
	id bigserial primary key,
	public_id text not null unique,
	capability_key text not null,
	version integer not null,
	risk_class text not null,
	approval_required boolean not null,
	input_schema jsonb not null,
	output_schema jsonb not null,
	input_schema_sha256 bytea not null,
	output_schema_sha256 bytea not null,
	executor_kind text not null,
	status text not null,
	revision bigint not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_integration_capabilities_key unique (capability_key, version),
	constraint matter_codex_integration_capabilities_public_id_check check (public_id ~ '^[a-z][a-z0-9_]{7,79}$'),
	constraint matter_codex_integration_capabilities_key_check check (capability_key = 'deployment.restart_workload' and version = 1),
	constraint matter_codex_integration_capabilities_risk_check check (risk_class = 'platform_admin' and approval_required),
	constraint matter_codex_integration_capabilities_schema_check check (
		jsonb_typeof(input_schema) = 'object' and jsonb_typeof(output_schema) = 'object'
		and octet_length(input_schema_sha256) = 32 and octet_length(output_schema_sha256) = 32
	),
	constraint matter_codex_integration_capabilities_executor_check check (executor_kind = 'recording_test'),
	constraint matter_codex_integration_capabilities_status_check check (status in ('active', 'disabled')),
	constraint matter_codex_integration_capabilities_revision_check check (revision > 0)
);

insert into matter_codex_integration_capabilities(
	public_id, capability_key, version, risk_class, approval_required,
	input_schema, output_schema, input_schema_sha256, output_schema_sha256,
	executor_kind, status, revision
)
values (
	'cap_deployment_restart_workload_v1',
	'deployment.restart_workload',
	1,
	'platform_admin',
	true,
	'{"additionalProperties":false,"properties":{"connection":{"type":"string"},"idempotency_key":{"type":"string"},"namespace":{"type":"string"},"workload_kind":{"const":"Deployment","type":"string"},"workload_name":{"type":"string"}},"required":["connection","namespace","workload_kind","workload_name","idempotency_key"],"type":"object"}'::jsonb,
	'{"additionalProperties":false,"properties":{"approval_id":{"type":"string"},"arguments_hash":{"type":"string"},"execution":{"type":["object","null"]},"invocation_id":{"type":"string"},"poll_after_seconds":{"type":"integer"},"reason_code":{"type":"string"},"status":{"enum":["pending","succeeded","rejected","expired","cancelled","failed"],"type":"string"}},"required":["status","invocation_id","approval_id","arguments_hash"],"type":"object"}'::jsonb,
	decode('14548b8e092de099d856e4523a3f032046e529ce21f92486562ffee0425a36fb', 'hex'),
	decode('3a7b9f0a7159ff2583694af3c2559323e722074633cf947d0d13f1bf3e26741f', 'hex'),
	'recording_test',
	'active',
	1
);

create table matter_codex_integration_connections (
	id bigserial primary key,
	public_id text not null unique,
	capability_id bigint not null references matter_codex_integration_capabilities(id) on delete restrict,
	installation_scope text not null,
	workspace_scope text not null,
	safe_config jsonb not null default '{}'::jsonb,
	credential_ref text not null default '',
	status text not null,
	revision bigint not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_integration_connections_capability_key unique (id, capability_id),
	constraint matter_codex_integration_connections_public_id_check check (public_id ~ '^[a-z][a-z0-9._-]{2,79}$'),
	constraint matter_codex_integration_connections_scope_check check (
		installation_scope = 'single-installation' and workspace_scope ~ '^[1-9][0-9]*$'
	),
	constraint matter_codex_integration_connections_config_check check (safe_config = '{}'::jsonb),
	constraint matter_codex_integration_connections_credential_check check (credential_ref = ''),
	constraint matter_codex_integration_connections_status_check check (status in ('active', 'disabled')),
	constraint matter_codex_integration_connections_revision_check check (revision > 0)
);

create table matter_codex_integration_grants (
	id bigserial primary key,
	public_id text not null unique,
	connection_id bigint not null,
	capability_id bigint not null,
	subject_kind text not null,
	subject_ref text not null,
	installation_scope text not null,
	workspace_scope text not null,
	session_scope text not null default '',
	allowed_namespace text not null,
	allowed_workload_kind text not null,
	allowed_workload_name text not null,
	enabled boolean not null,
	valid_from timestamptz not null,
	expires_at timestamptz,
	revision bigint not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_integration_grants_connection_fkey
		foreign key (connection_id, capability_id)
		references matter_codex_integration_connections(id, capability_id) on delete restrict,
	constraint matter_codex_integration_grants_public_id_check check (public_id ~ '^[a-z][a-z0-9._-]{2,79}$'),
	constraint matter_codex_integration_grants_subject_check check (subject_kind = 'agent_role' and subject_ref ~ '^[1-9][0-9]*$'),
	constraint matter_codex_integration_grants_scope_check check (
		installation_scope = 'single-installation' and workspace_scope ~ '^[1-9][0-9]*$'
	),
	constraint matter_codex_integration_grants_constraints_check check (
		allowed_namespace ~ '^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$'
		and allowed_workload_kind = 'Deployment'
		and allowed_workload_name ~ '^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$'
	),
	constraint matter_codex_integration_grants_validity_check check (expires_at is null or expires_at > valid_from),
	constraint matter_codex_integration_grants_revision_check check (revision > 0)
);

create index matter_codex_integration_grants_catalog_idx
	on matter_codex_integration_grants(subject_kind, subject_ref, workspace_scope, enabled, connection_id);

create table matter_codex_tool_invocations (
	id bigserial primary key,
	public_id text not null unique,
	session_id bigint not null references matter_codex_agent_sessions(id) on delete restrict,
	turn_id bigint not null references matter_codex_agent_session_turns(id) on delete restrict,
	project_id bigint not null references matter_codex_projects(id) on delete restrict,
	chat_id bigint not null references matter_codex_chats(id) on delete restrict,
	role_id bigint not null references matter_codex_agent_roles(id) on delete restrict,
	subject_kind text not null,
	subject_ref text not null,
	installation_scope text not null,
	workspace_scope text not null,
	session_scope text not null,
	session_token_secret_ref text not null,
	capability_id bigint not null references matter_codex_integration_capabilities(id) on delete restrict,
	capability_revision bigint not null,
	connection_id bigint not null,
	connection_revision bigint not null,
	grant_id bigint not null references matter_codex_integration_grants(id) on delete restrict,
	grant_revision bigint not null,
	idempotency_key text not null,
	arguments jsonb not null,
	arguments_sha256 bytea not null,
	approval_binding_sha256 bytea not null,
	correlation_id text not null,
	state text not null,
	reason_code text not null default '',
	execution_fence text not null default '',
	execution_lease_owner text not null default '',
	execution_lease_expires_at timestamptz,
	result jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	approved_at timestamptz,
	executing_at timestamptz,
	finished_at timestamptz,
	constraint matter_codex_tool_invocations_connection_fkey
		foreign key (connection_id, capability_id)
		references matter_codex_integration_connections(id, capability_id) on delete restrict,
	constraint matter_codex_tool_invocations_idempotency_key unique (session_id, capability_id, idempotency_key),
	constraint matter_codex_tool_invocations_approval_binding_key unique (id, approval_binding_sha256),
	constraint matter_codex_tool_invocations_public_id_check check (public_id ~ '^inv_[a-f0-9]{32}$'),
	constraint matter_codex_tool_invocations_scope_check check (
		installation_scope = 'single-installation' and workspace_scope = project_id::text and session_scope <> ''
	),
	constraint matter_codex_tool_invocations_subject_check check (subject_kind = 'agent_role' and subject_ref = role_id::text),
	constraint matter_codex_tool_invocations_idempotency_check check (idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$'),
	constraint matter_codex_tool_invocations_arguments_check check (
		jsonb_typeof(arguments) = 'object'
		and arguments ?& array['namespace', 'workload_kind', 'workload_name']
		and arguments - array['namespace', 'workload_kind', 'workload_name'] = '{}'::jsonb
		and arguments ->> 'workload_kind' = 'Deployment'
	),
	constraint matter_codex_tool_invocations_hash_check check (
		octet_length(arguments_sha256) = 32 and octet_length(approval_binding_sha256) = 32
	),
	constraint matter_codex_tool_invocations_correlation_check check (correlation_id ~ '^cor_[a-f0-9]{32}$'),
	constraint matter_codex_tool_invocations_state_check check (state in (
		'pending', 'approved', 'rejected', 'expired', 'cancelled', 'executing', 'succeeded', 'failed'
	)),
	constraint matter_codex_tool_invocations_lease_check check (
		(state = 'executing' and execution_fence <> '' and execution_lease_owner <> '' and execution_lease_expires_at is not null)
		or (state in ('succeeded', 'failed') and execution_fence <> '' and execution_lease_owner = '' and execution_lease_expires_at is null)
		or (state = 'cancelled' and execution_lease_owner = '' and execution_lease_expires_at is null)
		or (state not in ('executing', 'succeeded', 'failed', 'cancelled') and execution_fence = '' and execution_lease_owner = '' and execution_lease_expires_at is null)
	),
	constraint matter_codex_tool_invocations_result_check check (jsonb_typeof(result) = 'object')
);

create index matter_codex_tool_invocations_worker_idx
	on matter_codex_tool_invocations(state, execution_lease_expires_at, id)
	where state in ('approved', 'executing');

create table matter_codex_approval_requests (
	id bigserial primary key,
	public_id text not null unique,
	invocation_id bigint not null unique,
	approval_binding_sha256 bytea not null,
	state text not null,
	risk_class text not null,
	safe_preview jsonb not null,
	exact_approver_user_id text not null,
	exact_approver_user_name text not null default '',
	expires_at timestamptz not null,
	decision_actor_user_id text not null default '',
	decision_actor_user_name text not null default '',
	decision_at timestamptz,
	decision_reason text not null default '',
	mattermost_channel_id text not null,
	mattermost_root_post_id text not null,
	mattermost_post_id text not null default '',
	delivery_state text not null default 'pending',
	delivery_lease_owner text not null default '',
	delivery_lease_expires_at timestamptz,
	delivery_last_reason text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_approval_requests_invocation_fkey
		foreign key (invocation_id, approval_binding_sha256)
		references matter_codex_tool_invocations(id, approval_binding_sha256) on delete restrict,
	constraint matter_codex_approval_requests_public_id_check check (public_id ~ '^apr_[a-f0-9]{32}$'),
	constraint matter_codex_approval_requests_hash_check check (octet_length(approval_binding_sha256) = 32),
	constraint matter_codex_approval_requests_state_check check (state in ('pending', 'approved', 'rejected', 'expired', 'cancelled')),
	constraint matter_codex_approval_requests_risk_check check (risk_class = 'platform_admin'),
	constraint matter_codex_approval_requests_preview_check check (jsonb_typeof(safe_preview) = 'object'),
	constraint matter_codex_approval_requests_actor_check check (length(trim(exact_approver_user_id)) > 0),
	constraint matter_codex_approval_requests_mattermost_check check (
		length(trim(mattermost_channel_id)) > 0 and length(trim(mattermost_root_post_id)) > 0
	),
	constraint matter_codex_approval_requests_decision_check check (
		(state = 'pending' and decision_actor_user_id = '' and decision_at is null)
		or (state <> 'pending' and decision_actor_user_id <> '' and decision_at is not null)
	),
	constraint matter_codex_approval_requests_delivery_check check (
		delivery_state in ('pending', 'in_flight', 'delivered')
		and (delivery_state = 'in_flight') = (delivery_lease_owner <> '' and delivery_lease_expires_at is not null)
		and (delivery_state = 'delivered') = (mattermost_post_id <> '')
	)
);

create table matter_codex_integration_test_executions (
	invocation_id bigint primary key references matter_codex_tool_invocations(id) on delete restrict,
	execution_id text not null unique,
	execution_fence text not null,
	arguments_sha256 bytea not null,
	result jsonb not null,
	recorded_at timestamptz not null,
	constraint matter_codex_integration_test_executions_id_check check (execution_id ~ '^exec_[a-f0-9]{32}$'),
	constraint matter_codex_integration_test_executions_fence_check check (execution_fence ~ '^fence_[a-f0-9]{32}$'),
	constraint matter_codex_integration_test_executions_hash_check check (octet_length(arguments_sha256) = 32),
	constraint matter_codex_integration_test_executions_result_check check (jsonb_typeof(result) = 'object')
);

-- +goose StatementBegin
create function matter_codex_guard_tool_invocation()
returns trigger
language plpgsql
security definer
as $$
begin
	if row(
		new.public_id, new.session_id, new.turn_id, new.project_id, new.chat_id, new.role_id,
		new.subject_kind, new.subject_ref, new.installation_scope, new.workspace_scope, new.session_scope, new.session_token_secret_ref,
		new.capability_id, new.capability_revision, new.connection_id, new.connection_revision,
		new.grant_id, new.grant_revision, new.idempotency_key, new.arguments,
		new.arguments_sha256, new.approval_binding_sha256, new.correlation_id, new.created_at
	) is distinct from row(
		old.public_id, old.session_id, old.turn_id, old.project_id, old.chat_id, old.role_id,
		old.subject_kind, old.subject_ref, old.installation_scope, old.workspace_scope, old.session_scope, old.session_token_secret_ref,
		old.capability_id, old.capability_revision, old.connection_id, old.connection_revision,
		old.grant_id, old.grant_revision, old.idempotency_key, old.arguments,
		old.arguments_sha256, old.approval_binding_sha256, old.correlation_id, old.created_at
	) then
		raise exception 'tool invocation binding is immutable' using errcode = 'check_violation';
	end if;
	if old.state in ('rejected', 'expired', 'cancelled', 'succeeded', 'failed') and new.state <> old.state then
		raise exception 'tool invocation terminal state is irreversible' using errcode = 'check_violation';
	end if;
	if new.state <> old.state and not (
		(old.state = 'pending' and new.state in ('approved', 'rejected', 'expired', 'cancelled'))
		or (old.state = 'approved' and new.state in ('executing', 'failed'))
		or (old.state = 'executing' and new.state in ('succeeded', 'failed', 'cancelled'))
	) then
		raise exception 'tool invocation state transition is invalid' using errcode = 'check_violation';
	end if;
	return new;
end
$$;
-- +goose StatementEnd

create trigger matter_codex_tool_invocations_guard
before update on matter_codex_tool_invocations
for each row execute function matter_codex_guard_tool_invocation();

-- +goose StatementBegin
create function matter_codex_guard_approval_request()
returns trigger
language plpgsql
security definer
as $$
begin
	if row(
		new.public_id, new.invocation_id, new.approval_binding_sha256, new.risk_class,
		new.safe_preview, new.exact_approver_user_id, new.exact_approver_user_name,
		new.expires_at, new.mattermost_channel_id, new.mattermost_root_post_id, new.created_at
	) is distinct from row(
		old.public_id, old.invocation_id, old.approval_binding_sha256, old.risk_class,
		old.safe_preview, old.exact_approver_user_id, old.exact_approver_user_name,
		old.expires_at, old.mattermost_channel_id, old.mattermost_root_post_id, old.created_at
	) then
		raise exception 'approval request binding is immutable' using errcode = 'check_violation';
	end if;
	if old.state <> 'pending' and new.state <> old.state then
		raise exception 'approval request terminal state is irreversible' using errcode = 'check_violation';
	end if;
	if new.state <> old.state and not (old.state = 'pending' and new.state in ('approved', 'rejected', 'expired', 'cancelled')) then
		raise exception 'approval request state transition is invalid' using errcode = 'check_violation';
	end if;
	return new;
end
$$;
-- +goose StatementEnd

create trigger matter_codex_approval_requests_guard
before update on matter_codex_approval_requests
for each row execute function matter_codex_guard_approval_request();

-- +goose StatementBegin
create function matter_codex_guard_integration_test_execution()
returns trigger
language plpgsql
security definer
as $$
begin
	raise exception 'integration test execution receipt is immutable' using errcode = 'check_violation';
end
$$;
-- +goose StatementEnd

create trigger matter_codex_integration_test_executions_guard
before update or delete on matter_codex_integration_test_executions
for each row execute function matter_codex_guard_integration_test_execution();

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
	table_name text;
	sequence_name text;
begin
	foreach table_name in array array[
		'matter_codex_integration_capabilities', 'matter_codex_integration_connections',
		'matter_codex_integration_grants', 'matter_codex_tool_invocations',
		'matter_codex_approval_requests', 'matter_codex_integration_test_executions'
	]
	loop
		execute format('revoke all on table %I.%I from public', trusted_schema, table_name);
	end loop;
	foreach sequence_name in array array[
		'matter_codex_integration_capabilities_id_seq', 'matter_codex_integration_connections_id_seq',
		'matter_codex_integration_grants_id_seq', 'matter_codex_tool_invocations_id_seq',
		'matter_codex_approval_requests_id_seq'
	]
	loop
		execute format('revoke all on sequence %I.%I from public', trusted_schema, sequence_name);
	end loop;
	execute format('alter function %I.matter_codex_guard_tool_invocation() set search_path = pg_catalog, %I, pg_temp', trusted_schema, trusted_schema);
	execute format('alter function %I.matter_codex_guard_approval_request() set search_path = pg_catalog, %I, pg_temp', trusted_schema, trusted_schema);
	execute format('alter function %I.matter_codex_guard_integration_test_execution() set search_path = pg_catalog, %I, pg_temp', trusted_schema, trusted_schema);
	execute format('revoke all on function %I.matter_codex_guard_tool_invocation() from public', trusted_schema);
	execute format('revoke all on function %I.matter_codex_guard_approval_request() from public', trusted_schema);
	execute format('revoke all on function %I.matter_codex_guard_integration_test_execution() from public', trusted_schema);
	if runtime_role_name is not null then
		execute format('grant select on table %I.matter_codex_integration_capabilities to %I', trusted_schema, runtime_role_name);
		execute format('grant select on table %I.matter_codex_integration_connections to %I', trusted_schema, runtime_role_name);
		execute format('grant select on table %I.matter_codex_integration_grants to %I', trusted_schema, runtime_role_name);
		execute format('grant select, insert, update on table %I.matter_codex_tool_invocations to %I', trusted_schema, runtime_role_name);
		execute format('grant select, insert, update on table %I.matter_codex_approval_requests to %I', trusted_schema, runtime_role_name);
		execute format('grant select, insert on table %I.matter_codex_integration_test_executions to %I', trusted_schema, runtime_role_name);
		execute format('grant usage, select on sequence %I.matter_codex_tool_invocations_id_seq to %I', trusted_schema, runtime_role_name);
		execute format('grant usage, select on sequence %I.matter_codex_approval_requests_id_seq to %I', trusted_schema, runtime_role_name);
	end if;
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000030 is forward-only: integration approval evidence cannot be removed safely';
end
$$;
-- +goose StatementEnd
