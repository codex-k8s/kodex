update matter_codex_scheduled_runs
set status = 'failed',
	outcome = 'failed',
	safe_summary = $2,
	finished_at = $3,
	updated_at = $3
where id = $1;
