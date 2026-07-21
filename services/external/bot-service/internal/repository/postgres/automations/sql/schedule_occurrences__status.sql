update matter_codex_schedule_occurrences
set status = $2,
	updated_at = $3
where id = $1;
