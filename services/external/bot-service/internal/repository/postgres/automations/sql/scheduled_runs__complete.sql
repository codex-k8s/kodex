update matter_codex_scheduled_runs
set status = $2,
	outcome = $3,
	safe_summary = $4,
	callback_payload_sha256 = $5,
	finished_at = $6,
	updated_at = $6
where id = $1;
