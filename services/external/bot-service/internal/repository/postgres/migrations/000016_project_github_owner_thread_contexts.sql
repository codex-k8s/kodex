-- +goose Up
alter table matter_codex_projects
	add column if not exists github_owner text not null default '',
	add column if not exists github_owner_type text not null default '';

create index if not exists matter_codex_projects_github_owner_idx
	on matter_codex_projects(github_owner)
	where github_owner <> '';

create table if not exists matter_codex_thread_contexts (
	id bigserial primary key,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	chat_id bigint not null references matter_codex_chats(id) on delete cascade,
	mattermost_channel_id text not null,
	mattermost_root_post_id text not null,
	repository_id bigint references matter_codex_repositories(id) on delete set null,
	status text not null default 'pending',
	pending_mattermost_post_id text not null default '',
	pending_user_id text not null default '',
	pending_user_name text not null default '',
	pending_message text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (chat_id, mattermost_root_post_id)
);

create index if not exists matter_codex_thread_contexts_chat_idx
	on matter_codex_thread_contexts(chat_id, updated_at desc);

-- +goose Down
drop table if exists matter_codex_thread_contexts;

drop index if exists matter_codex_projects_github_owner_idx;

alter table matter_codex_projects
	drop column if exists github_owner_type,
	drop column if exists github_owner;
