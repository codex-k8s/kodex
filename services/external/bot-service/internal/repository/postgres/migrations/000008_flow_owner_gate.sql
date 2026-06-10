-- +goose Up
alter table matter_codex_agent_flows
	add column if not exists owner_user_id text not null default '',
	add column if not exists owner_user text not null default '',
	add column if not exists control_channel_id text not null default '',
	add column if not exists control_post_id text not null default '',
	add column if not exists action_token text not null default '',
	add column if not exists owner_decision text not null default '';

create index if not exists matter_codex_agent_flows_control_post_idx
	on matter_codex_agent_flows(control_post_id)
	where control_post_id <> '';

-- +goose Down
drop index if exists matter_codex_agent_flows_control_post_idx;

alter table matter_codex_agent_flows
	drop column if exists owner_decision,
	drop column if exists action_token,
	drop column if exists control_post_id,
	drop column if exists control_channel_id,
	drop column if exists owner_user,
	drop column if exists owner_user_id;
