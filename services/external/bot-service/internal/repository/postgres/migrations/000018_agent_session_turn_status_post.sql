-- +goose Up
alter table matter_codex_agent_session_turns
	add column if not exists mattermost_status_post_id text not null default '';

create index if not exists matter_codex_agent_session_turns_status_post_idx
	on matter_codex_agent_session_turns(mattermost_status_post_id)
	where mattermost_status_post_id <> '';

-- +goose Down
drop index if exists matter_codex_agent_session_turns_status_post_idx;

alter table matter_codex_agent_session_turns
	drop column if exists mattermost_status_post_id;
