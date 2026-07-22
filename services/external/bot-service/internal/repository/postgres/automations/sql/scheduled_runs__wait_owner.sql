-- name: scheduled_runs__wait_owner :exec
update matter_codex_scheduled_runs
set status = 'waiting_owner',
	outcome = 'requires_human',
	safe_summary = $2,
	callback_payload_sha256 = $3,
	finished_at = null,
	updated_at = $4
where id = $1;
