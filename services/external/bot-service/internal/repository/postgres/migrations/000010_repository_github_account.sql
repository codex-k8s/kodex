-- +goose Up
alter table matter_codex_repositories
	add column if not exists github_account_name text not null default 'primary';

-- +goose Down
alter table matter_codex_repositories
	drop column if exists github_account_name;
