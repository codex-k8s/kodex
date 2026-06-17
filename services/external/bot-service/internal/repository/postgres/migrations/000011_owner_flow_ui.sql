-- +goose Up
alter table matter_codex_agent_profiles
	add column if not exists kubernetes_access text not null default 'read-only',
	add column if not exists sandbox_mode text not null default 'danger-full-access',
	add column if not exists config_overlay text not null default '';

alter table matter_codex_agent_flows
	add column if not exists developer_profile_name text not null default 'developer',
	add column if not exists reviewer_profile_name text not null default 'reviewer',
	add column if not exists flow_preset text not null default 'developer_review';

create index if not exists matter_codex_agent_flows_status_updated_idx
	on matter_codex_agent_flows(status, updated_at desc);

create index if not exists matter_codex_agent_runs_status_updated_idx
	on matter_codex_agent_runs(status, updated_at desc);

-- +goose Down
drop index if exists matter_codex_agent_runs_status_updated_idx;
drop index if exists matter_codex_agent_flows_status_updated_idx;

alter table matter_codex_agent_flows
	drop column if exists flow_preset,
	drop column if exists reviewer_profile_name,
	drop column if exists developer_profile_name;

alter table matter_codex_agent_profiles
	drop column if exists config_overlay,
	drop column if exists sandbox_mode,
	drop column if exists kubernetes_access;
