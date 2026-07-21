insert into matter_codex_schedule_occurrences (
	public_id,
	schedule_id,
	project_id,
	source,
	idempotency_key,
	scheduled_for,
	status
)
values ($1, $2, $3, 'manual', $4, $5, 'queued')
on conflict (schedule_id, idempotency_key)
do update set idempotency_key = excluded.idempotency_key
returning id, public_id, (xmax = 0) as created;
