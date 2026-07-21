-- +goose Up
create extension if not exists pgcrypto with schema public;

-- До создания partial unique индексов явно отклоняем неоднозначные внешние
-- привязки. В диагностике нет пользовательских значений.
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from matter_codex_projects
		where trim(mattermost_team_id) <> ''
		group by mattermost_team_id
		having count(*) > 1
	) then
		raise exception 'MCV30_DUPLICATE_MATTERMOST_TEAM_BINDINGS'
			using errcode = 'unique_violation';
	end if;
	if exists (
		select 1
		from matter_codex_chats
		where trim(mattermost_channel_id) <> ''
		group by mattermost_channel_id
		having count(*) > 1
	) then
		raise exception 'MCV30_DUPLICATE_MATTERMOST_CHANNEL_BINDINGS'
			using errcode = 'unique_violation';
	end if;
end
$$;
-- +goose StatementEnd

create table matter_codex_workspaces (
	id bigserial primary key,
	organization_scope text not null,
	legacy_project_id bigint not null unique,
	name text not null,
	slug text not null,
	description text not null default '',
	mattermost_team_id text not null default '',
	status text not null default 'active',
	managed_by text not null default 'ui',
	source_ref jsonb not null default '{}'::jsonb,
	source_revision text not null default '',
	provenance jsonb not null default '{}'::jsonb,
	record_version bigint not null default 1,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_workspaces_legacy_project_fkey
		foreign key (legacy_project_id) references matter_codex_projects(id) on delete restrict,
	constraint matter_codex_workspaces_scope_check check (trim(organization_scope) <> ''),
	constraint matter_codex_workspaces_slug_check check (trim(slug) <> ''),
	constraint matter_codex_workspaces_status_check check (status in ('active', 'disabled', 'archived')),
	constraint matter_codex_workspaces_managed_by_check check (managed_by in ('ui', 'git')),
	constraint matter_codex_workspaces_version_check check (record_version > 0),
	unique (organization_scope, id),
	unique (organization_scope, slug)
);

create unique index matter_codex_workspaces_team_binding_idx
	on matter_codex_workspaces(organization_scope, mattermost_team_id)
	where trim(mattermost_team_id) <> '';

create table matter_codex_rooms (
	id bigserial primary key,
	organization_scope text not null,
	workspace_id bigint not null,
	legacy_chat_id bigint not null unique,
	name text not null,
	slug text not null,
	description text not null default '',
	room_type text not null default 'custom',
	purpose text not null default 'custom',
	work_policy text not null default '',
	mattermost_channel_id text not null default '',
	status text not null default 'active',
	managed_by text not null default 'ui',
	source_ref jsonb not null default '{}'::jsonb,
	source_revision text not null default '',
	provenance jsonb not null default '{}'::jsonb,
	record_version bigint not null default 1,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_rooms_workspace_fkey
		foreign key (organization_scope, workspace_id)
		references matter_codex_workspaces(organization_scope, id) on delete restrict,
	constraint matter_codex_rooms_legacy_chat_fkey
		foreign key (legacy_chat_id) references matter_codex_chats(id) on delete restrict,
	constraint matter_codex_rooms_scope_check check (trim(organization_scope) <> ''),
	constraint matter_codex_rooms_slug_check check (trim(slug) <> ''),
	constraint matter_codex_rooms_status_check check (status in ('active', 'disabled', 'archived')),
	constraint matter_codex_rooms_managed_by_check check (managed_by in ('ui', 'git')),
	constraint matter_codex_rooms_version_check check (record_version > 0),
	unique (organization_scope, id),
	unique (organization_scope, workspace_id, id),
	unique (organization_scope, workspace_id, slug)
);

create unique index matter_codex_rooms_channel_binding_idx
	on matter_codex_rooms(organization_scope, mattermost_channel_id)
	where trim(mattermost_channel_id) <> '';

create table matter_codex_role_definitions (
	id bigserial primary key,
	organization_scope text not null,
	legacy_agent_role_id bigint not null unique,
	name text not null,
	slug text not null,
	role_type text not null,
	description text not null default '',
	default_policy jsonb not null default '{}'::jsonb,
	status text not null default 'active',
	managed_by text not null default 'ui',
	source_ref jsonb not null default '{}'::jsonb,
	source_revision text not null default '',
	provenance jsonb not null default '{}'::jsonb,
	record_version bigint not null default 1,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_role_definitions_legacy_role_fkey
		foreign key (legacy_agent_role_id) references matter_codex_agent_roles(id) on delete restrict,
	constraint matter_codex_role_definitions_scope_check check (trim(organization_scope) <> ''),
	constraint matter_codex_role_definitions_slug_check check (trim(slug) <> ''),
	constraint matter_codex_role_definitions_role_type_check check (trim(role_type) <> ''),
	constraint matter_codex_role_definitions_status_check check (status in ('active', 'disabled', 'archived')),
	constraint matter_codex_role_definitions_managed_by_check check (managed_by in ('ui', 'git')),
	constraint matter_codex_role_definitions_version_check check (record_version > 0),
	unique (organization_scope, id),
	unique (organization_scope, slug)
);

create table matter_codex_instruction_sets (
	id bigserial primary key,
	organization_scope text not null,
	name text not null,
	slug text not null,
	source_type text not null default 'ui_markdown',
	managed_by text not null default 'ui',
	source_ref jsonb not null default '{}'::jsonb,
	source_revision text not null default '',
	provenance jsonb not null default '{}'::jsonb,
	current_version_id bigint,
	status text not null default 'active',
	record_version bigint not null default 1,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_instruction_sets_scope_check check (trim(organization_scope) <> ''),
	constraint matter_codex_instruction_sets_slug_check check (trim(slug) <> ''),
	constraint matter_codex_instruction_sets_source_type_check
		check (source_type in ('ui_markdown', 'git', 'gitops', 'artifact')),
	constraint matter_codex_instruction_sets_managed_by_check check (managed_by in ('ui', 'git')),
	constraint matter_codex_instruction_sets_status_check check (status in ('active', 'disabled', 'archived')),
	constraint matter_codex_instruction_sets_version_check check (record_version > 0),
	unique (organization_scope, id),
	unique (organization_scope, slug)
);

create table matter_codex_instruction_versions (
	id bigserial primary key,
	organization_scope text not null,
	instruction_set_id bigint not null,
	version bigint not null,
	markdown text not null,
	content_sha256 bytea not null,
	actor_ref text not null,
	created_at timestamptz not null default now(),
	constraint matter_codex_instruction_versions_set_fkey
		foreign key (organization_scope, instruction_set_id)
		references matter_codex_instruction_sets(organization_scope, id) on delete restrict,
	constraint matter_codex_instruction_versions_scope_check check (trim(organization_scope) <> ''),
	constraint matter_codex_instruction_versions_number_check check (version > 0),
	constraint matter_codex_instruction_versions_markdown_check
		check (octet_length(convert_to(markdown, 'UTF8')) <= 262144),
	constraint matter_codex_instruction_versions_hash_length_check check (octet_length(content_sha256) = 32),
	constraint matter_codex_instruction_versions_hash_check
		check (content_sha256 = public.digest(convert_to(markdown, 'UTF8'), 'sha256')),
	constraint matter_codex_instruction_versions_actor_check check (trim(actor_ref) <> ''),
	unique (instruction_set_id, version),
	unique (organization_scope, instruction_set_id, id)
);

alter table matter_codex_instruction_sets
	add constraint matter_codex_instruction_sets_current_version_fkey
	foreign key (organization_scope, id, current_version_id)
	references matter_codex_instruction_versions(organization_scope, instruction_set_id, id)
	on delete restrict deferrable initially deferred;

create table matter_codex_agents (
	id bigserial primary key,
	organization_scope text not null,
	legacy_agent_role_id bigint not null unique,
	role_definition_id bigint not null,
	instruction_set_id bigint,
	bot_identity_id bigint,
	name text not null,
	slug text not null,
	status text not null default 'active',
	managed_by text not null default 'ui',
	source_ref jsonb not null default '{}'::jsonb,
	source_revision text not null default '',
	provenance jsonb not null default '{}'::jsonb,
	record_version bigint not null default 1,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_agents_legacy_role_fkey
		foreign key (legacy_agent_role_id) references matter_codex_agent_roles(id) on delete restrict,
	constraint matter_codex_agents_role_definition_fkey
		foreign key (organization_scope, role_definition_id)
		references matter_codex_role_definitions(organization_scope, id) on delete restrict,
	constraint matter_codex_agents_instruction_set_fkey
		foreign key (organization_scope, instruction_set_id)
		references matter_codex_instruction_sets(organization_scope, id) on delete restrict,
	constraint matter_codex_agents_bot_identity_fkey
		foreign key (bot_identity_id) references matter_codex_mattermost_bot_identities(id) on delete restrict,
	constraint matter_codex_agents_scope_check check (trim(organization_scope) <> ''),
	constraint matter_codex_agents_slug_check check (trim(slug) <> ''),
	constraint matter_codex_agents_status_check check (status in ('active', 'disabled', 'archived')),
	constraint matter_codex_agents_managed_by_check check (managed_by in ('ui', 'git')),
	constraint matter_codex_agents_version_check check (record_version > 0),
	unique (organization_scope, id),
	unique (organization_scope, slug)
);

create unique index matter_codex_agents_bot_identity_binding_idx
	on matter_codex_agents(bot_identity_id)
	where bot_identity_id is not null;

create table matter_codex_agent_assignments (
	id bigserial primary key,
	organization_scope text not null,
	agent_id bigint not null,
	workspace_id bigint not null,
	room_id bigint,
	enabled boolean not null default true,
	is_default boolean not null default false,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint matter_codex_agent_assignments_agent_fkey
		foreign key (organization_scope, agent_id)
		references matter_codex_agents(organization_scope, id) on delete restrict,
	constraint matter_codex_agent_assignments_workspace_fkey
		foreign key (organization_scope, workspace_id)
		references matter_codex_workspaces(organization_scope, id) on delete restrict,
	constraint matter_codex_agent_assignments_room_fkey
		foreign key (organization_scope, workspace_id, room_id)
		references matter_codex_rooms(organization_scope, workspace_id, id) on delete restrict,
	constraint matter_codex_agent_assignments_scope_check check (trim(organization_scope) <> ''),
	unique nulls not distinct (agent_id, workspace_id, room_id)
);

-- +goose StatementBegin
create function matter_codex_guard_instruction_version()
returns trigger
language plpgsql
security definer
as $$
begin
	raise exception 'instruction version is immutable'
		using errcode = 'check_violation';
end
$$;
-- +goose StatementEnd

create trigger matter_codex_instruction_version_guard
before update or delete on matter_codex_instruction_versions
for each row execute function matter_codex_guard_instruction_version();

insert into matter_codex_workspaces(
	organization_scope, legacy_project_id, name, slug, description,
	mattermost_team_id, status, managed_by, source_revision, record_version,
	created_at, updated_at
)
select
	'installation', project.id, project.name, project.slug, project.description,
	project.mattermost_team_id, 'active', 'ui', 'legacy-project:' || project.id::text, 1,
	project.created_at, project.updated_at
from matter_codex_projects project
order by project.id;

insert into matter_codex_rooms(
	organization_scope, workspace_id, legacy_chat_id, name, slug, description,
	room_type, purpose, work_policy, mattermost_channel_id, status, managed_by,
	source_revision, record_version, created_at, updated_at
)
select
	workspace.organization_scope, workspace.id, chat.id, chat.name, chat.slug, chat.description,
	chat.chat_type, chat.system_purpose, chat.work_policy, chat.mattermost_channel_id,
	'active', 'ui', 'legacy-chat:' || chat.id::text, 1, chat.created_at, chat.updated_at
from matter_codex_chats chat
join matter_codex_workspaces workspace on workspace.legacy_project_id = chat.project_id
order by chat.id;

insert into matter_codex_role_definitions(
	organization_scope, legacy_agent_role_id, name, slug, role_type, description,
	default_policy, status, managed_by, source_revision, record_version, created_at, updated_at
)
select
	'installation', role.id, role.name, 'legacy-role-' || role.id::text,
	role.role_type, role.description,
	jsonb_build_object('prompt_mode', role.prompt_mode),
	case when role.enabled then 'active' else 'disabled' end,
	'ui', 'legacy-agent-role:' || role.id::text, 1, role.created_at, role.updated_at
from matter_codex_agent_roles role
order by role.id;

insert into matter_codex_instruction_sets(
	organization_scope, name, slug, source_type, managed_by, source_revision,
	status, record_version, created_at, updated_at
)
select
	'installation', role.name, 'agent-' || role.id::text, 'ui_markdown', 'ui',
	'legacy-agent-role:' || role.id::text,
	case when role.enabled then 'active' else 'disabled' end,
	1, role.created_at, role.updated_at
from matter_codex_agent_roles role
where length(coalesce(role.prompt_template, '')) > 0
order by role.id;

insert into matter_codex_instruction_versions(
	organization_scope, instruction_set_id, version, markdown, content_sha256,
	actor_ref, created_at
)
select
	instruction_set.organization_scope, instruction_set.id, 1, role.prompt_template,
	public.digest(convert_to(role.prompt_template, 'UTF8'), 'sha256'),
	'migration-000030', role.updated_at
from matter_codex_agent_roles role
join matter_codex_instruction_sets instruction_set
	on instruction_set.slug = 'agent-' || role.id::text
where length(coalesce(role.prompt_template, '')) > 0
order by role.id;

update matter_codex_instruction_sets instruction_set
set current_version_id = version.id
from matter_codex_instruction_versions version
where version.instruction_set_id = instruction_set.id
	and version.version = 1;

insert into matter_codex_agents(
	organization_scope, legacy_agent_role_id, role_definition_id,
	instruction_set_id, bot_identity_id, name, slug, status, managed_by,
	source_revision, record_version, created_at, updated_at
)
select
	role_definition.organization_scope,
	role.id,
	role_definition.id,
	instruction_set.id,
	bot_identity.id,
	role.name,
	'legacy-agent-' || role.id::text,
	case when role.enabled then 'active' else 'disabled' end,
	'ui',
	'legacy-agent-role:' || role.id::text,
	1,
	role.created_at,
	role.updated_at
from matter_codex_agent_roles role
join matter_codex_role_definitions role_definition
	on role_definition.legacy_agent_role_id = role.id
left join matter_codex_instruction_sets instruction_set
	on instruction_set.slug = 'agent-' || role.id::text
left join matter_codex_mattermost_bot_identities bot_identity
	on bot_identity.role_id = role.id
order by role.id;

insert into matter_codex_agent_assignments(
	organization_scope, agent_id, workspace_id, room_id, enabled, is_default
)
select
	agent.organization_scope, agent.id, workspace.id, null, role.enabled, false
from matter_codex_agent_roles role
join matter_codex_agents agent on agent.legacy_agent_role_id = role.id
join matter_codex_workspaces workspace on workspace.legacy_project_id = role.project_id
order by role.id;

insert into matter_codex_agent_assignments(
	organization_scope, agent_id, workspace_id, room_id, enabled, is_default
)
select
	agent.organization_scope, agent.id, room.workspace_id, room.id, participant.enabled, false
from matter_codex_chat_participants participant
join matter_codex_agents agent on agent.legacy_agent_role_id = participant.role_id
join matter_codex_rooms room on room.legacy_chat_id = participant.chat_id
order by participant.id;

-- +goose StatementBegin
do $$
declare
	trusted_schema text := current_schema();
	runtime_role_name text := nullif(trim(current_setting('matter_codex.runtime_role', true)), '');
	table_name text;
	sequence_name text;
begin
	execute format(
		'alter function %I.matter_codex_guard_instruction_version() set search_path = pg_catalog, %I, pg_temp',
		trusted_schema, trusted_schema
	);
	execute format(
		'revoke all on function %I.matter_codex_guard_instruction_version() from public',
		trusted_schema
	);
	foreach table_name in array array[
		'matter_codex_workspaces',
		'matter_codex_rooms',
		'matter_codex_role_definitions',
		'matter_codex_agents',
		'matter_codex_agent_assignments',
		'matter_codex_instruction_sets',
		'matter_codex_instruction_versions'
	] loop
		execute format('revoke all on table %I.%I from public', trusted_schema, table_name);
		if runtime_role_name is not null then
			if table_name = 'matter_codex_instruction_versions' then
				execute format('grant select, insert on table %I.%I to %I', trusted_schema, table_name, runtime_role_name);
			else
				execute format('grant select, insert, update on table %I.%I to %I', trusted_schema, table_name, runtime_role_name);
			end if;
		end if;
	end loop;
	if runtime_role_name is not null then
		foreach sequence_name in array array[
			'matter_codex_workspaces_id_seq',
			'matter_codex_rooms_id_seq',
			'matter_codex_role_definitions_id_seq',
			'matter_codex_agents_id_seq',
			'matter_codex_agent_assignments_id_seq',
			'matter_codex_instruction_sets_id_seq',
			'matter_codex_instruction_versions_id_seq'
		] loop
			execute format('grant usage, select on sequence %I.%I to %I', trusted_schema, sequence_name, runtime_role_name);
		end loop;
	end if;
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000030 is forward-only: universal model and instruction history cannot be removed safely';
end
$$;
-- +goose StatementEnd
