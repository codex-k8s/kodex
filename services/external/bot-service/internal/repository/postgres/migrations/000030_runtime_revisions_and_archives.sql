-- +goose Up
create table matter_codex_runtime_revisions (
	id bigserial primary key,
	digest text not null unique,
	manifest jsonb not null,
	account_alias text not null,
	authorization_revision text not null,
	created_at timestamptz not null default now(),
	constraint matter_codex_runtime_revisions_digest_check
		check (digest ~ '^[0-9a-f]{64}$'),
	constraint matter_codex_runtime_revisions_manifest_check
		check (jsonb_typeof(manifest) = 'object'),
	constraint matter_codex_runtime_revisions_account_check
		check (length(trim(account_alias)) > 0 and length(trim(authorization_revision)) > 0)
);

alter table matter_codex_agent_sessions
	add column desired_runtime_revision_id bigint references matter_codex_runtime_revisions(id),
	add column applied_runtime_revision_id bigint references matter_codex_runtime_revisions(id),
	add column archive_version bigint not null default 0,
	add column archive_sha256 text not null default '',
	add column archive_size_bytes bigint not null default 0,
	add constraint matter_codex_agent_sessions_archive_metadata_check check (
		(archive_version = 0 and archive_sha256 = '' and archive_size_bytes = 0)
		or (
			archive_version > 0
			and archive_sha256 ~ '^[0-9a-f]{64}$'
			and archive_size_bytes >= 0
			and archive_size_bytes <= 50331648
		)
	);

alter table matter_codex_agent_session_turns
	add column runtime_revision_id bigint references matter_codex_runtime_revisions(id);

create index matter_codex_agent_sessions_desired_revision_idx
	on matter_codex_agent_sessions(desired_runtime_revision_id)
	where desired_runtime_revision_id is not null;

create index matter_codex_agent_sessions_applied_revision_idx
	on matter_codex_agent_sessions(applied_runtime_revision_id)
	where applied_runtime_revision_id is not null;

create index matter_codex_agent_session_turns_runtime_revision_idx
	on matter_codex_agent_session_turns(runtime_revision_id)
	where runtime_revision_id is not null;

create table matter_codex_agent_session_archives (
	id bigserial primary key,
	session_id bigint not null references matter_codex_agent_sessions(id) on delete restrict,
	version bigint not null,
	codex_session_id text not null,
	payload_gzip_base64 text not null,
	sha256 text not null,
	size_bytes bigint not null,
	created_at timestamptz not null default now(),
	unique (session_id, version),
	constraint matter_codex_agent_session_archives_version_check check (version > 0),
	constraint matter_codex_agent_session_archives_payload_check check (
		length(payload_gzip_base64) > 0 and length(payload_gzip_base64) <= 67108864
	),
	constraint matter_codex_agent_session_archives_sha256_check check (sha256 ~ '^[0-9a-f]{64}$'),
	constraint matter_codex_agent_session_archives_size_check check (
		size_bytes >= 0 and size_bytes <= 50331648
	)
);

create index matter_codex_agent_session_archives_latest_idx
	on matter_codex_agent_session_archives(session_id, version desc);

-- +goose StatementBegin
create function matter_codex_guard_runtime_immutable()
returns trigger
language plpgsql
as $$
begin
	raise exception 'runtime immutable row cannot be changed'
		using errcode = 'check_violation';
end
$$;
-- +goose StatementEnd

create trigger matter_codex_runtime_revisions_immutable
before update or delete on matter_codex_runtime_revisions
for each row execute function matter_codex_guard_runtime_immutable();

create trigger matter_codex_agent_session_archives_immutable
before update or delete on matter_codex_agent_session_archives
for each row execute function matter_codex_guard_runtime_immutable();

-- +goose StatementBegin
create function matter_codex_guard_agent_session_account_affinity()
returns trigger
language plpgsql
as $$
begin
	if length(trim(old.openai_account_name)) > 0
		and new.openai_account_name is distinct from old.openai_account_name
	then
		raise exception 'agent session account affinity is immutable'
			using errcode = 'check_violation';
	end if;
	return new;
end
$$;
-- +goose StatementEnd

create trigger matter_codex_agent_sessions_account_affinity
before update of openai_account_name on matter_codex_agent_sessions
for each row execute function matter_codex_guard_agent_session_account_affinity();

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
begin
	execute format(
		'alter function %I.matter_codex_guard_runtime_immutable() set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'alter function %I.matter_codex_guard_agent_session_account_affinity() set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format('revoke all on table %I.matter_codex_runtime_revisions from public', trusted_schema);
	execute format('revoke all on table %I.matter_codex_agent_session_archives from public', trusted_schema);
	execute format('revoke all on function %I.matter_codex_guard_runtime_immutable() from public', trusted_schema);
	execute format('revoke all on function %I.matter_codex_guard_agent_session_account_affinity() from public', trusted_schema);
	if runtime_role_name is not null then
		execute format(
			'grant select, insert on table %I.matter_codex_runtime_revisions to %I',
			trusted_schema, runtime_role_name
		);
		execute format(
			'grant usage, select on sequence %I.matter_codex_runtime_revisions_id_seq to %I',
			trusted_schema, runtime_role_name
		);
		execute format(
			'grant select, insert on table %I.matter_codex_agent_session_archives to %I',
			trusted_schema, runtime_role_name
		);
		execute format(
			'grant usage, select on sequence %I.matter_codex_agent_session_archives_id_seq to %I',
			trusted_schema, runtime_role_name
		);
		execute format(
			'grant update (desired_runtime_revision_id, applied_runtime_revision_id, archive_version, archive_sha256, archive_size_bytes) on table %I.matter_codex_agent_sessions to %I',
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
	raise exception 'migration 000030 is forward-only: runtime revisions and confirmed archives cannot be removed safely';
end
$$;
-- +goose StatementEnd
