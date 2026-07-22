-- name: scheduled_runs__resolve_owner :exec
update matter_codex_scheduled_runs
set status = 'succeeded',
	finished_at = $2,
	updated_at = $2
where id = $1
	and status = 'waiting_owner'
	and outcome = 'requires_human'
	and callback_payload_sha256 is not null;
