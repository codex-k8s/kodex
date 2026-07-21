update matter_codex_scheduled_runs
set callback_revoked_at = $3,
	updated_at = $3
where public_id = $1
	and project_id = $2
	and callback_revoked_at is null;
