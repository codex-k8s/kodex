-- +goose Up
create extension if not exists vector;

alter table matter_codex_chats
	add column if not exists system_purpose text not null default 'custom';

-- +goose StatementBegin
do $$
declare
	constraint_name text;
begin
	for constraint_name in
		select constraints.conname
		from pg_constraint constraints
		where constraints.conrelid = 'matter_codex_agent_delegations'::regclass
			and constraints.contype = 'u'
			and pg_get_constraintdef(constraints.oid) = 'UNIQUE (source_session_id, work_item_key)'
	loop
		execute format('alter table matter_codex_agent_delegations drop constraint %I', constraint_name);
	end loop;
end $$;
-- +goose StatementEnd

create unique index if not exists matter_codex_agent_delegations_source_turn_work_item_uidx
	on matter_codex_agent_delegations(source_turn_id, work_item_key);

create index if not exists matter_codex_chats_project_purpose_idx
	on matter_codex_chats(project_id, system_purpose)
	where system_purpose <> 'custom';

create table if not exists matter_codex_policy_revisions (
	id bigserial primary key,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	version bigint not null,
	status text not null default 'draft',
	settings jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	activated_at timestamptz,
	unique (project_id, version)
);

create unique index if not exists matter_codex_policy_revisions_active_idx
	on matter_codex_policy_revisions(project_id)
	where status = 'active';

create table if not exists matter_codex_role_capabilities (
	id bigserial primary key,
	policy_revision_id bigint not null references matter_codex_policy_revisions(id) on delete cascade,
	role_id bigint not null references matter_codex_agent_roles(id) on delete cascade,
	capability text not null,
	constraints jsonb not null default '{}'::jsonb,
	enabled boolean not null default true,
	created_at timestamptz not null default now(),
	unique (policy_revision_id, role_id, capability)
);

create table if not exists matter_codex_role_relationship_policies (
	id bigserial primary key,
	policy_revision_id bigint not null references matter_codex_policy_revisions(id) on delete cascade,
	source_role_id bigint not null references matter_codex_agent_roles(id) on delete cascade,
	action text not null,
	target_role_id bigint not null references matter_codex_agent_roles(id) on delete cascade,
	constraints jsonb not null default '{}'::jsonb,
	enabled boolean not null default true,
	created_at timestamptz not null default now(),
	unique (policy_revision_id, source_role_id, action, target_role_id)
);

create index if not exists matter_codex_role_relationship_lookup_idx
	on matter_codex_role_relationship_policies(policy_revision_id, source_role_id, action, target_role_id)
	where enabled;

create table if not exists matter_codex_process_runs (
	id bigserial primary key,
	public_id text not null unique,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	policy_revision_id bigint not null references matter_codex_policy_revisions(id),
	root_role_id bigint not null references matter_codex_agent_roles(id),
	root_initiator_user_id text not null,
	root_initiator_user_name text not null,
	root_trigger_post_id text not null,
	root_channel_id text not null,
	root_thread_post_id text not null,
	status text not null default 'running',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	finished_at timestamptz
);

create index if not exists matter_codex_process_runs_project_status_idx
	on matter_codex_process_runs(project_id, status, updated_at desc);

create table if not exists matter_codex_process_turns (
	turn_id bigint primary key references matter_codex_agent_session_turns(id) on delete cascade,
	process_run_id bigint not null references matter_codex_process_runs(id) on delete cascade,
	parent_turn_id bigint references matter_codex_agent_session_turns(id) on delete set null,
	launch_post_id text not null default '',
	created_at timestamptz not null default now()
);

create index if not exists matter_codex_process_turns_run_idx
	on matter_codex_process_turns(process_run_id, turn_id);

create table if not exists matter_codex_work_claims (
	id bigserial primary key,
	process_run_id bigint not null references matter_codex_process_runs(id) on delete cascade,
	turn_id bigint not null unique references matter_codex_agent_session_turns(id) on delete cascade,
	role_id bigint not null references matter_codex_agent_roles(id),
	summary text not null default '',
	domains text[] not null default '{}',
	resource_keys text[] not null default '{}',
	links jsonb not null default '[]'::jsonb,
	status text not null default 'active',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create index if not exists matter_codex_work_claims_active_idx
	on matter_codex_work_claims(process_run_id, updated_at desc)
	where status = 'active';

create index if not exists matter_codex_work_claims_resources_idx
	on matter_codex_work_claims using gin(resource_keys);

create table if not exists matter_codex_owner_attention_requests (
	id bigserial primary key,
	process_run_id bigint not null references matter_codex_process_runs(id) on delete cascade,
	turn_id bigint not null references matter_codex_agent_session_turns(id) on delete cascade,
	severity text not null,
	summary text not null,
	options jsonb not null default '[]'::jsonb,
	recommendation text not null default '',
	evidence_links jsonb not null default '[]'::jsonb,
	pause_scope text not null default 'turn',
	idempotency_key text not null,
	status text not null default 'open',
	mattermost_post_id text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (process_run_id, idempotency_key)
);

create table if not exists matter_codex_memory_records (
	id bigserial primary key,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	scope text not null,
	role_id bigint references matter_codex_agent_roles(id) on delete cascade,
	status text not null default 'active',
	importance text not null default 'normal',
	created_by_role_id bigint not null references matter_codex_agent_roles(id),
	source_turn_id bigint references matter_codex_agent_session_turns(id) on delete set null,
	source_post_id text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	check (scope in ('project', 'role')),
	check ((scope = 'project' and role_id is null) or (scope = 'role' and role_id is not null))
);

create table if not exists matter_codex_memory_record_versions (
	id bigserial primary key,
	record_id bigint not null references matter_codex_memory_records(id) on delete cascade,
	version integer not null,
	title text not null,
	content text not null,
	content_hash text not null,
	supersedes_version_id bigint references matter_codex_memory_record_versions(id) on delete set null,
	search_document tsvector generated always as (to_tsvector('simple', coalesce(title, '') || ' ' || coalesce(content, ''))) stored,
	created_at timestamptz not null default now(),
	unique (record_id, version),
	unique (record_id, content_hash)
);

create index if not exists matter_codex_memory_versions_search_idx
	on matter_codex_memory_record_versions using gin(search_document);

create table if not exists matter_codex_memory_embeddings (
	version_id bigint primary key references matter_codex_memory_record_versions(id) on delete cascade,
	model_revision text not null,
	dimensions integer not null,
	embedding vector not null,
	indexed_at timestamptz not null default now()
);

insert into matter_codex_policy_revisions (project_id, version, status, activated_at)
select projects.id, 1, 'active', now()
from matter_codex_projects projects
where not exists (
	select 1 from matter_codex_policy_revisions revisions where revisions.project_id = projects.id
);

insert into matter_codex_role_capabilities (policy_revision_id, role_id, capability)
select revisions.id, roles.id, capability
from matter_codex_policy_revisions revisions
join matter_codex_agent_roles roles on roles.project_id = revisions.project_id and roles.enabled
cross join lateral unnest(array[
	'callbacks.return',
	'memory.project.read',
	'memory.role.read',
	'memory.role.write',
	'work.project.read',
	'work.own.update',
	'sync.request'
]::text[]) capability
where revisions.status = 'active'
on conflict do nothing;

insert into matter_codex_role_capabilities (policy_revision_id, role_id, capability)
select revisions.id, roles.id, capability
from matter_codex_policy_revisions revisions
join matter_codex_agent_roles roles on roles.project_id = revisions.project_id and roles.enabled
cross join lateral unnest(array[
	'agents.start',
	'callbacks.receive',
	'owner_attention.request',
	'memory.project.write',
	'work.project.manage',
	'sync.receive'
]::text[]) capability
where revisions.status = 'active'
	and lower(coalesce(nullif(roles.role_type, ''), roles.name)) in ('manager', 'director', 'coordinator')
on conflict do nothing;

insert into matter_codex_role_relationship_policies (policy_revision_id, source_role_id, action, target_role_id)
select revisions.id, source.id, 'start', target.id
from matter_codex_policy_revisions revisions
join matter_codex_agent_roles source on source.project_id = revisions.project_id and source.enabled
join matter_codex_agent_roles target on target.project_id = revisions.project_id and target.enabled and target.id <> source.id
where revisions.status = 'active'
	and lower(coalesce(nullif(source.role_type, ''), source.name)) in ('manager', 'director', 'coordinator')
on conflict do nothing;

insert into matter_codex_role_relationship_policies (policy_revision_id, source_role_id, action, target_role_id)
select revisions.id, source.id, 'callback', target.id
from matter_codex_policy_revisions revisions
join matter_codex_agent_roles source on source.project_id = revisions.project_id and source.enabled
join matter_codex_agent_roles target on target.project_id = revisions.project_id and target.enabled
where revisions.status = 'active'
	and source.id <> target.id
	and lower(coalesce(nullif(target.role_type, ''), target.name)) in ('manager', 'director', 'coordinator')
on conflict do nothing;

-- +goose Down
drop table if exists matter_codex_memory_embeddings;
drop table if exists matter_codex_memory_record_versions;
drop table if exists matter_codex_memory_records;
drop table if exists matter_codex_owner_attention_requests;
drop table if exists matter_codex_work_claims;
drop table if exists matter_codex_process_turns;
drop table if exists matter_codex_process_runs;
drop table if exists matter_codex_role_relationship_policies;
drop table if exists matter_codex_role_capabilities;
drop table if exists matter_codex_policy_revisions;
drop index if exists matter_codex_agent_delegations_source_turn_work_item_uidx;
alter table matter_codex_agent_delegations
	add constraint matter_codex_agent_delegations_source_session_work_item_uniq
	unique (source_session_id, work_item_key);
drop index if exists matter_codex_chats_project_purpose_idx;
alter table matter_codex_chats drop column if exists system_purpose;
-- The vector extension can be shared by other schemas in the same database.
-- A down migration intentionally leaves it installed.
