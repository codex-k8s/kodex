-- +goose Up
alter table matter_codex_chats
	add column if not exists status text not null default 'active',
	add column if not exists archived_at timestamptz;

alter table matter_codex_chats
	drop constraint if exists matter_codex_chats_status_check;

alter table matter_codex_chats
	add constraint matter_codex_chats_status_check
	check (
		(status = 'active' and archived_at is null)
		or (status = 'archived' and archived_at is not null)
	);

create index if not exists matter_codex_chats_active_project_idx
	on matter_codex_chats(project_id, updated_at desc)
	where status = 'active';

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000036 is forward-only: archived chat lifecycle state cannot be removed safely';
end
$$;
-- +goose StatementEnd
