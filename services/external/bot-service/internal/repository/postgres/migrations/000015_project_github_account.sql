-- +goose Up
alter table matter_codex_projects
	add column if not exists github_account_name text not null default '';

create index if not exists matter_codex_projects_github_account_idx
	on matter_codex_projects(github_account_name)
	where github_account_name <> '';

-- +goose Down
drop index if exists matter_codex_projects_github_account_idx;

alter table matter_codex_projects
	drop column if exists github_account_name;
