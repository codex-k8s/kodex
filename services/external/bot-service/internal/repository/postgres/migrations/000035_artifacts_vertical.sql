-- +goose Up
alter table matter_codex_thread_contexts
	add column pending_mattermost_file_ids text[] not null default '{}'::text[]
	check (cardinality(pending_mattermost_file_ids) <= 8);

create table matter_codex_artifacts (
	id text primary key,
	project_id bigint not null references matter_codex_projects(id) on delete restrict,
	chat_id bigint not null references matter_codex_chats(id) on delete restrict,
	session_id bigint not null references matter_codex_agent_sessions(id) on delete restrict,
	role_id bigint not null references matter_codex_agent_roles(id) on delete restrict,
	runtime_turn_id bigint not null references matter_codex_agent_session_turns(id) on delete restrict,
	turn_id text not null check (length(trim(turn_id)) between 1 and 200),
	direction text not null check (direction in ('inbound', 'outbound')),
	mattermost_post_id text not null default '' check (octet_length(mattermost_post_id) <= 200),
	mattermost_file_id text not null default '' check (octet_length(mattermost_file_id) <= 200),
	retention_policy_version text not null default 'artifact-retention-v1',
	retention_until timestamptz not null,
	retention_hold boolean not null default true,
	created_at timestamptz not null default now(),
	constraint matter_codex_artifacts_id_check check (id ~ '^[0-9a-f]{32}$'),
	constraint matter_codex_artifacts_source_check check (
		direction = 'outbound' or (length(trim(mattermost_post_id)) > 0 and length(trim(mattermost_file_id)) > 0)
	)
);

create unique index matter_codex_artifacts_inbound_source_unique
	on matter_codex_artifacts(project_id, chat_id, session_id, mattermost_post_id, mattermost_file_id)
	where direction = 'inbound';

create table matter_codex_artifact_versions (
	id text primary key,
	artifact_id text not null references matter_codex_artifacts(id) on delete restrict,
	version_number integer not null default 1 check (version_number = 1),
	storage_key text not null check (length(storage_key) between 1 and 500),
	original_name text not null check (octet_length(original_name) between 1 and 1024),
	safe_name text not null check (octet_length(safe_name) <= 300),
	media_type text not null check (media_type in (
		'text/plain', 'text/markdown', 'text/csv', 'application/json', 'application/pdf',
		'image/png', 'image/jpeg', 'image/webp', 'image/gif',
		'application/zip', 'application/x-tar', 'application/gzip',
		'application/msword', 'application/vnd.ms-excel', 'application/vnd.ms-powerpoint',
		'application/vnd.oasis.opendocument.text',
		'application/vnd.oasis.opendocument.text-template',
		'application/vnd.oasis.opendocument.text-master',
		'application/vnd.oasis.opendocument.text-master-template',
		'application/vnd.oasis.opendocument.text-web',
		'application/vnd.oasis.opendocument.spreadsheet',
		'application/vnd.oasis.opendocument.spreadsheet-template',
		'application/vnd.oasis.opendocument.presentation',
		'application/vnd.oasis.opendocument.presentation-template',
		'application/vnd.oasis.opendocument.graphics',
		'application/vnd.oasis.opendocument.graphics-template',
		'application/vnd.oasis.opendocument.chart',
		'application/vnd.oasis.opendocument.chart-template',
		'application/vnd.oasis.opendocument.image',
		'application/vnd.oasis.opendocument.image-template',
		'application/vnd.oasis.opendocument.formula',
		'application/vnd.oasis.opendocument.formula-template',
		'application/vnd.oasis.opendocument.base',
		'application/vnd.oasis.opendocument.database',
		'application/vnd.sun.xml.writer', 'application/vnd.sun.xml.writer.template',
		'application/vnd.sun.xml.writer.global', 'application/vnd.sun.xml.calc',
		'application/vnd.sun.xml.calc.template', 'application/vnd.sun.xml.impress',
		'application/vnd.sun.xml.impress.template', 'application/vnd.sun.xml.draw',
		'application/vnd.sun.xml.draw.template', 'application/vnd.sun.xml.math',
		'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
		'application/vnd.openxmlformats-officedocument.wordprocessingml.template',
		'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
		'application/vnd.openxmlformats-officedocument.spreadsheetml.template',
		'application/vnd.openxmlformats-officedocument.presentationml.presentation',
		'application/vnd.openxmlformats-officedocument.presentationml.template',
		'application/vnd.openxmlformats-officedocument.presentationml.slideshow',
		'application/vnd.openxmlformats-officedocument.presentationml.slide'
	)),
	declared_media_type text not null default '' check (octet_length(declared_media_type) <= 200),
	size_bytes bigint not null check (size_bytes between 0 and 8388608),
	sha256 text not null check (sha256 ~ '^[0-9a-f]{64}$'),
	state text not null check (state in ('uploading', 'scanning', 'available', 'quarantined', 'failed')),
	error_code text not null default '' check (octet_length(error_code) <= 100),
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_artifact_versions_id_check check (id ~ '^[0-9a-f]{32}$'),
	constraint matter_codex_artifact_versions_number_unique unique (artifact_id, version_number),
	constraint matter_codex_artifact_versions_storage_unique unique (storage_key),
	constraint matter_codex_artifact_versions_state_error_check check (
		(state in ('uploading', 'scanning', 'available') and error_code = '')
		or (state in ('quarantined', 'failed') and error_code ~ '^[a-z][a-z0-9_]{0,99}$')
	)
);

create table matter_codex_message_artifact_bindings (
	id bigserial primary key,
	artifact_version_id text not null references matter_codex_artifact_versions(id) on delete restrict,
	project_id bigint not null references matter_codex_projects(id) on delete restrict,
	chat_id bigint not null references matter_codex_chats(id) on delete restrict,
	session_id bigint not null references matter_codex_agent_sessions(id) on delete restrict,
	role_id bigint not null references matter_codex_agent_roles(id) on delete restrict,
	runtime_turn_id bigint not null references matter_codex_agent_session_turns(id) on delete restrict,
	turn_id text not null check (length(trim(turn_id)) between 1 and 200),
	mattermost_post_id text not null default '' check (octet_length(mattermost_post_id) <= 200),
	mattermost_file_id text not null default '' check (octet_length(mattermost_file_id) <= 200),
	direction text not null check (direction in ('inbound', 'outbound')),
	ordinal integer not null check (ordinal between 1 and 8),
	created_at timestamptz not null default now(),
	constraint matter_codex_message_artifact_bindings_turn_unique
		unique (artifact_version_id, project_id, chat_id, session_id, role_id, runtime_turn_id, turn_id),
	constraint matter_codex_message_artifact_bindings_source_check check (
		direction = 'outbound' or (length(trim(mattermost_post_id)) > 0 and length(trim(mattermost_file_id)) > 0)
	)
);

create index matter_codex_message_artifact_bindings_turn_idx
	on matter_codex_message_artifact_bindings(project_id, chat_id, session_id, role_id, runtime_turn_id, turn_id, ordinal);

create table matter_codex_artifact_deliveries (
	id text primary key,
	artifact_version_id text not null references matter_codex_artifact_versions(id) on delete restrict,
	project_id bigint not null references matter_codex_projects(id) on delete restrict,
	chat_id bigint not null references matter_codex_chats(id) on delete restrict,
	session_id bigint not null references matter_codex_agent_sessions(id) on delete restrict,
	role_id bigint not null references matter_codex_agent_roles(id) on delete restrict,
	runtime_turn_id bigint not null references matter_codex_agent_session_turns(id) on delete restrict,
	turn_id text not null check (length(trim(turn_id)) between 1 and 200),
	idempotency_key text not null check (length(trim(idempotency_key)) between 1 and 200),
	bot_token_secret_ref text not null check (length(trim(bot_token_secret_ref)) between 1 and 253),
	state text not null check (state in ('pending', 'delivered', 'failed', 'quarantined')),
	mattermost_file_id text not null default '' check (octet_length(mattermost_file_id) <= 200),
	mattermost_post_id text not null default '' check (octet_length(mattermost_post_id) <= 200),
	error_code text not null default '' check (octet_length(error_code) <= 100),
	attempts integer not null default 0 check (attempts between 0 and 1000),
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_artifact_deliveries_id_check check (id ~ '^[0-9a-f]{32}$'),
	constraint matter_codex_artifact_deliveries_scope_unique
		unique (project_id, chat_id, session_id, role_id, runtime_turn_id, turn_id, idempotency_key),
	constraint matter_codex_artifact_deliveries_delivered_check check (
		state <> 'delivered' or (length(trim(mattermost_file_id)) > 0 and length(trim(mattermost_post_id)) > 0)
	),
	constraint matter_codex_artifact_deliveries_state_error_check check (
		(state in ('pending', 'delivered') and error_code = '')
		or (state in ('failed', 'quarantined') and error_code ~ '^[a-z][a-z0-9_]{0,99}$')
	)
);

-- +goose StatementBegin
create function matter_codex_guard_artifact_row()
returns trigger
language plpgsql
security definer
as $$
begin
	if tg_op <> 'INSERT' then
		raise exception 'artifact metadata is immutable' using errcode = 'check_violation';
	end if;
	if not exists (
		select 1
		from matter_codex_agent_session_turns turn
		join matter_codex_agent_sessions session on session.id = turn.session_id
		where turn.id = new.runtime_turn_id and turn.run_id = new.turn_id
			and turn.session_id = new.session_id and session.project_id = new.project_id
			and session.chat_id = new.chat_id and session.role_id = new.role_id
			and turn.status in ('admitting', 'queued', 'running', 'capacity_retry')
	) then
		raise exception 'artifact scope is invalid' using errcode = 'check_violation';
	end if;
	return new;
end
$$;
-- +goose StatementEnd

-- +goose StatementBegin
create function matter_codex_guard_artifact_version()
returns trigger
language plpgsql
security definer
as $$
begin
	if tg_op = 'DELETE' then
		raise exception 'artifact version is immutable' using errcode = 'check_violation';
	end if;
	if tg_op = 'INSERT' then
		if not exists (
			select 1 from matter_codex_artifacts a
			where a.id = new.artifact_id
				and new.storage_key = format('projects/%s/sessions/%s/artifacts/%s/versions/%s', a.project_id, a.session_id, a.id, new.id)
		) then
			raise exception 'artifact version storage scope is invalid' using errcode = 'check_violation';
		end if;
		return new;
	end if;
	if row(
		new.id, new.artifact_id, new.version_number, new.storage_key, new.original_name,
		new.safe_name, new.media_type, new.declared_media_type, new.size_bytes,
		new.sha256, new.created_at
	) is distinct from row(
		old.id, old.artifact_id, old.version_number, old.storage_key, old.original_name,
		old.safe_name, old.media_type, old.declared_media_type, old.size_bytes,
		old.sha256, old.created_at
	) then
		raise exception 'artifact version content metadata is immutable' using errcode = 'check_violation';
	end if;
	if old.state in ('available', 'quarantined', 'failed') and row(new.state, new.error_code) is distinct from row(old.state, old.error_code) then
		raise exception 'terminal artifact version state is immutable' using errcode = 'check_violation';
	end if;
	if old.state = 'uploading' and new.state not in ('uploading', 'scanning', 'quarantined', 'failed') then
		raise exception 'invalid artifact version state transition' using errcode = 'check_violation';
	end if;
	if old.state = 'scanning' and new.state not in ('scanning', 'available', 'quarantined', 'failed') then
		raise exception 'invalid artifact version state transition' using errcode = 'check_violation';
	end if;
	return new;
end
$$;
-- +goose StatementEnd

-- +goose StatementBegin
create function matter_codex_guard_artifact_binding()
returns trigger
language plpgsql
security definer
as $$
declare
	artifact_direction text;
	artifact_post_id text;
	artifact_file_id text;
	version_size bigint;
	bound_inbound_count bigint;
	bound_total_bytes bigint;
begin
	if tg_op <> 'INSERT' then
		raise exception 'artifact binding is immutable' using errcode = 'check_violation';
	end if;
	select a.direction, a.mattermost_post_id, a.mattermost_file_id, version.size_bytes
	into artifact_direction, artifact_post_id, artifact_file_id, version_size
	from matter_codex_artifact_versions version
	join matter_codex_artifacts a on a.id = version.artifact_id
	where version.id = new.artifact_version_id
		and a.project_id = new.project_id and a.chat_id = new.chat_id and a.session_id = new.session_id
		and a.role_id = new.role_id and a.direction = new.direction;
	if not found then
		raise exception 'artifact binding scope is invalid' using errcode = 'check_violation';
	end if;
	if not exists (
		select 1
		from matter_codex_agent_session_turns turn
		join matter_codex_agent_sessions session on session.id = turn.session_id
		where turn.id = new.runtime_turn_id and turn.run_id = new.turn_id
			and turn.session_id = new.session_id and session.project_id = new.project_id
			and session.chat_id = new.chat_id and session.role_id = new.role_id
			and turn.status in ('admitting', 'queued', 'running', 'capacity_retry')
	) then
		raise exception 'artifact binding scope is invalid' using errcode = 'check_violation';
	end if;
	if artifact_direction = 'inbound' and (
		artifact_post_id <> new.mattermost_post_id or artifact_file_id <> new.mattermost_file_id
	) then
		raise exception 'artifact binding source is invalid' using errcode = 'check_violation';
	end if;
	if artifact_direction = 'outbound' and (
		length(trim(new.mattermost_post_id)) > 0 or length(trim(new.mattermost_file_id)) > 0
	) then
		raise exception 'outbound artifact binding source is invalid' using errcode = 'check_violation';
	end if;
	if exists (
		select 1 from matter_codex_message_artifact_bindings binding
		where binding.artifact_version_id = new.artifact_version_id
			and binding.project_id = new.project_id and binding.chat_id = new.chat_id
			and binding.session_id = new.session_id and binding.role_id = new.role_id
			and binding.runtime_turn_id = new.runtime_turn_id and binding.turn_id = new.turn_id
			and binding.mattermost_post_id = new.mattermost_post_id
			and binding.mattermost_file_id = new.mattermost_file_id
			and binding.direction = new.direction and binding.ordinal = new.ordinal
	) then
		return new;
	end if;
	perform pg_advisory_xact_lock(hashtextextended(format(
		'artifact-turn:%s:%s:%s:%s:%s:%s', new.project_id, new.chat_id, new.session_id, new.role_id, new.runtime_turn_id, new.turn_id
	), 0));
	select
		count(*) filter (where binding.direction = 'inbound'),
		coalesce(sum(version.size_bytes), 0)
	into bound_inbound_count, bound_total_bytes
	from matter_codex_message_artifact_bindings binding
	join matter_codex_artifact_versions version on version.id = binding.artifact_version_id
	where binding.project_id = new.project_id and binding.chat_id = new.chat_id
		and binding.session_id = new.session_id and binding.role_id = new.role_id
		and binding.runtime_turn_id = new.runtime_turn_id and binding.turn_id = new.turn_id;
	if new.direction = 'inbound' and bound_inbound_count >= 8 then
		raise exception 'artifact turn file limit exceeded' using errcode = 'check_violation';
	end if;
	if bound_total_bytes + version_size > 33554432 then
		raise exception 'artifact turn byte limit exceeded' using errcode = 'check_violation';
	end if;
	return new;
end
$$;
-- +goose StatementEnd

-- +goose StatementBegin
create function matter_codex_guard_artifact_delivery()
returns trigger
language plpgsql
security definer
as $$
begin
	if tg_op = 'DELETE' then
		raise exception 'artifact delivery is immutable' using errcode = 'check_violation';
	end if;
	if tg_op = 'INSERT' then
		if not exists (
			select 1
			from matter_codex_artifact_versions version
			join matter_codex_artifacts a on a.id = version.artifact_id
			join matter_codex_agent_session_turns turn on turn.id = new.runtime_turn_id
			where version.id = new.artifact_version_id and a.direction = 'outbound'
				and a.project_id = new.project_id and a.chat_id = new.chat_id and a.session_id = new.session_id
				and a.role_id = new.role_id and a.runtime_turn_id = new.runtime_turn_id and a.turn_id = new.turn_id
				and turn.session_id = new.session_id and turn.run_id = new.turn_id and turn.status = 'running'
		) then
			raise exception 'artifact delivery scope is invalid' using errcode = 'check_violation';
		end if;
		return new;
	end if;
	if row(
			new.id, new.artifact_version_id, new.project_id, new.chat_id, new.session_id,
			new.role_id, new.runtime_turn_id, new.turn_id, new.idempotency_key, new.bot_token_secret_ref, new.created_at
		) is distinct from row(
			old.id, old.artifact_version_id, old.project_id, old.chat_id, old.session_id,
			old.role_id, old.runtime_turn_id, old.turn_id, old.idempotency_key, old.bot_token_secret_ref, old.created_at
	) then
		raise exception 'artifact delivery identity is immutable' using errcode = 'check_violation';
	end if;
	if old.state in ('delivered', 'quarantined') and row(
		new.state, new.mattermost_file_id, new.mattermost_post_id, new.error_code, new.attempts
	) is distinct from row(
		old.state, old.mattermost_file_id, old.mattermost_post_id, old.error_code, old.attempts
	) then
		raise exception 'terminal artifact delivery is immutable' using errcode = 'check_violation';
	end if;
	if old.state in ('pending', 'failed') and new.state not in ('pending', 'failed', 'delivered') then
		raise exception 'invalid artifact delivery state transition' using errcode = 'check_violation';
	end if;
	if new.attempts < old.attempts or new.attempts > old.attempts + 1 then
		raise exception 'invalid artifact delivery attempt transition' using errcode = 'check_violation';
	end if;
	if length(trim(old.mattermost_file_id)) > 0 and new.mattermost_file_id <> old.mattermost_file_id then
		raise exception 'artifact delivery file identity is immutable' using errcode = 'check_violation';
	end if;
	if length(trim(old.mattermost_post_id)) > 0 and new.mattermost_post_id <> old.mattermost_post_id then
		raise exception 'artifact delivery post identity is immutable' using errcode = 'check_violation';
	end if;
	return new;
end
$$;
-- +goose StatementEnd

create trigger matter_codex_artifacts_guard
before insert or update or delete on matter_codex_artifacts
for each row execute function matter_codex_guard_artifact_row();

create trigger matter_codex_artifact_versions_guard
before insert or update or delete on matter_codex_artifact_versions
for each row execute function matter_codex_guard_artifact_version();

create trigger matter_codex_message_artifact_bindings_guard
before insert or update or delete on matter_codex_message_artifact_bindings
for each row execute function matter_codex_guard_artifact_binding();

create trigger matter_codex_artifact_deliveries_guard
before insert or update or delete on matter_codex_artifact_deliveries
for each row execute function matter_codex_guard_artifact_delivery();

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
begin
	execute format('alter function %I.matter_codex_guard_artifact_row() set search_path = pg_catalog, %I, pg_temp', trusted_schema, trusted_schema);
	execute format('alter function %I.matter_codex_guard_artifact_version() set search_path = pg_catalog, %I, pg_temp', trusted_schema, trusted_schema);
	execute format('alter function %I.matter_codex_guard_artifact_binding() set search_path = pg_catalog, %I, pg_temp', trusted_schema, trusted_schema);
	execute format('alter function %I.matter_codex_guard_artifact_delivery() set search_path = pg_catalog, %I, pg_temp', trusted_schema, trusted_schema);
	execute format('revoke all on table %I.matter_codex_artifacts from public', trusted_schema);
	execute format('revoke all on table %I.matter_codex_artifact_versions from public', trusted_schema);
	execute format('revoke all on table %I.matter_codex_message_artifact_bindings from public', trusted_schema);
	execute format('revoke all on table %I.matter_codex_artifact_deliveries from public', trusted_schema);
	execute format('revoke all on function %I.matter_codex_guard_artifact_row() from public', current_schema());
	execute format('revoke all on function %I.matter_codex_guard_artifact_version() from public', current_schema());
	execute format('revoke all on function %I.matter_codex_guard_artifact_binding() from public', current_schema());
	execute format('revoke all on function %I.matter_codex_guard_artifact_delivery() from public', current_schema());
	if runtime_role_name is not null then
		execute format('grant select, insert on table %I.matter_codex_artifacts to %I', current_schema(), runtime_role_name);
		execute format('grant select, insert on table %I.matter_codex_artifact_versions to %I', current_schema(), runtime_role_name);
		execute format('grant update (state, error_code, updated_at) on table %I.matter_codex_artifact_versions to %I', current_schema(), runtime_role_name);
		execute format('grant select, insert on table %I.matter_codex_message_artifact_bindings to %I', current_schema(), runtime_role_name);
		execute format('grant usage, select on sequence %I.matter_codex_message_artifact_bindings_id_seq to %I', current_schema(), runtime_role_name);
		execute format('grant select, insert on table %I.matter_codex_artifact_deliveries to %I', current_schema(), runtime_role_name);
		execute format('grant update (state, mattermost_file_id, mattermost_post_id, error_code, attempts, updated_at) on table %I.matter_codex_artifact_deliveries to %I', current_schema(), runtime_role_name);
	end if;
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000035 is forward-only: immutable artifact metadata and retention holds cannot be removed safely';
end
$$;
-- +goose StatementEnd
