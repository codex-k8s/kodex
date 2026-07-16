-- +goose Up
create table if not exists matter_codex_agent_delegations (
	id bigserial primary key,
	project_id bigint not null references matter_codex_projects(id) on delete cascade,
	source_session_id bigint not null references matter_codex_agent_sessions(id) on delete cascade,
	source_turn_id bigint not null references matter_codex_agent_session_turns(id) on delete cascade,
	target_chat_id bigint not null references matter_codex_chats(id) on delete cascade,
	target_role_id bigint not null references matter_codex_agent_roles(id) on delete cascade,
	target_root_post_id text not null default '',
	target_session_id bigint references matter_codex_agent_sessions(id) on delete set null,
	target_turn_id bigint references matter_codex_agent_session_turns(id) on delete set null,
	target_run_id text not null default '',
	work_item_key text not null,
	title text not null,
	status text not null default 'creating',
	callback_turn_id bigint references matter_codex_agent_session_turns(id) on delete set null,
	callback_run_id text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (source_session_id, work_item_key)
);

create index if not exists matter_codex_agent_delegations_source_idx
	on matter_codex_agent_delegations(source_session_id, updated_at desc);

create index if not exists matter_codex_agent_delegations_target_idx
	on matter_codex_agent_delegations(target_session_id, updated_at desc)
	where target_session_id is not null;

-- +goose Down
drop table if exists matter_codex_agent_delegations;
