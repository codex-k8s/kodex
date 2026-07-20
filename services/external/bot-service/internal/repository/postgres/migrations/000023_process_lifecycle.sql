-- +goose Up
alter table matter_codex_owner_attention_requests
	add column if not exists resolved_at timestamptz,
	add column if not exists resolved_by_user_id text not null default '',
	add column if not exists resolved_by_post_id text not null default '';

update matter_codex_process_runs process
set status = 'waiting_owner', updated_at = now(), finished_at = null
where exists (
	select 1
	from matter_codex_owner_attention_requests attention
	where attention.process_run_id = process.id and attention.status = 'open'
);

update matter_codex_process_runs process
set status = 'completed', updated_at = now(), finished_at = coalesce(process.finished_at, now())
where not exists (
	select 1
	from matter_codex_owner_attention_requests attention
	where attention.process_run_id = process.id and attention.status = 'open'
)
	and not exists (
		select 1
		from matter_codex_process_turns process_turn
		join matter_codex_agent_session_turns turn on turn.id = process_turn.turn_id
		where process_turn.process_run_id = process.id
			and turn.status in ('queued', 'running', 'capacity_retry')
	);

-- +goose Down
update matter_codex_process_runs
set status = 'running', finished_at = null, updated_at = now()
where status in ('waiting_owner', 'completed');

alter table matter_codex_owner_attention_requests
	drop column if exists resolved_by_post_id,
	drop column if exists resolved_by_user_id,
	drop column if exists resolved_at;
