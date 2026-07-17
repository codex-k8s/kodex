-- +goose Up
alter table matter_codex_projects
	add column if not exists mattermost_runs_channel_id text not null default '';

alter table matter_codex_agent_sessions
	add column if not exists openai_account_name text not null default '';

update matter_codex_agent_sessions s
set openai_account_name = r.openai_account_name
from matter_codex_agent_roles r
where r.id = s.role_id
	and s.openai_account_name = '';

create index if not exists matter_codex_agent_sessions_openai_account_idx
	on matter_codex_agent_sessions(openai_account_name)
	where openai_account_name <> '';

alter table matter_codex_agent_session_turns
	add column if not exists parent_turn_ids bigint[] not null default '{}',
	add column if not exists trigger_post_ids text[] not null default '{}',
	add column if not exists initiator_user_names text[] not null default '{}',
	add column if not exists mattermost_runs_post_id text not null default '';

update matter_codex_agent_session_turns
set trigger_post_ids = case when mattermost_post_id = '' then '{}'::text[] else array[mattermost_post_id] end,
	initiator_user_names = case when user_name = '' then '{}'::text[] else array[user_name] end
where trigger_post_ids = '{}'::text[]
	and initiator_user_names = '{}'::text[];

create index if not exists matter_codex_agent_session_turns_parent_idx
	on matter_codex_agent_session_turns using gin(parent_turn_ids);

create index if not exists matter_codex_agent_session_turns_runs_post_idx
	on matter_codex_agent_session_turns(mattermost_runs_post_id)
	where mattermost_runs_post_id <> '';

-- +goose Down
drop index if exists matter_codex_agent_session_turns_runs_post_idx;
drop index if exists matter_codex_agent_session_turns_parent_idx;
alter table matter_codex_agent_session_turns
	drop column if exists mattermost_runs_post_id,
	drop column if exists initiator_user_names,
	drop column if exists trigger_post_ids,
	drop column if exists parent_turn_ids;
drop index if exists matter_codex_agent_sessions_openai_account_idx;
alter table matter_codex_agent_sessions
	drop column if exists openai_account_name;
alter table matter_codex_projects
	drop column if exists mattermost_runs_channel_id;
