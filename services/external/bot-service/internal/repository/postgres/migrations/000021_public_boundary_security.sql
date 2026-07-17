-- +goose Up
create table if not exists matter_codex_interaction_capabilities (
	token_hash bytea primary key,
	kind text not null,
	operation text not null,
	resource_type text not null default '',
	resource_id text not null default '',
	channel_id text not null,
	post_binding text not null,
	actor_user_id text not null,
	actor_user_name text not null default '',
	installation_scope text not null,
	workspace_scope text not null default '',
	session_scope text not null default '',
	context_hash bytea not null,
	status text not null default 'unused',
	issued_at timestamptz not null,
	expires_at timestamptz not null,
	consumed_at timestamptz,
	constraint matter_codex_interaction_capabilities_kind_check
		check (kind in ('action', 'dialog')),
	constraint matter_codex_interaction_capabilities_status_check
		check (status in ('unused', 'consumed', 'revoked')),
	constraint matter_codex_interaction_capabilities_token_hash_check
		check (octet_length(token_hash) = 32),
	constraint matter_codex_interaction_capabilities_context_hash_check
		check (octet_length(context_hash) = 32),
	constraint matter_codex_interaction_capabilities_expiry_check
		check (expires_at > issued_at),
	constraint matter_codex_interaction_capabilities_consumed_check
		check ((status = 'consumed' and consumed_at is not null) or (status <> 'consumed' and consumed_at is null))
);

create index if not exists matter_codex_interaction_capabilities_expiry_idx
	on matter_codex_interaction_capabilities(status, expires_at);

-- +goose Down
drop table if exists matter_codex_interaction_capabilities;
