-- +goose Up
alter table matter_codex_github_accounts
	add column if not exists scopes text not null default '';

-- +goose Down
alter table matter_codex_github_accounts
	drop column if exists scopes;
