-- +goose Up
create index if not exists matter_codex_chats_channel_idx
	on matter_codex_chats(mattermost_channel_id)
	where mattermost_channel_id <> '';

-- +goose Down
drop index if exists matter_codex_chats_channel_idx;
