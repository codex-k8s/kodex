insert into matter_codex_automation_audit_events (
	project_id,
	schedule_id,
	scheduled_run_id,
	event_type,
	actor_user_id,
	actor_user_name,
	correlation_id,
	safe_summary
)
values ($1, $2, $3, $4, $5, $6, $7, $8)
on conflict (schedule_id, scheduled_run_id, event_type)
do nothing;
