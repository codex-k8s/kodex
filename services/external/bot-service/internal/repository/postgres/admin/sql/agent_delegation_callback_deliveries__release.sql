-- name: agent_delegation_callback_deliveries__release :one
update matter_codex_agent_delegation_callback_deliveries set
	status = $3,
	lease_owner = null,
	lease_expires_at = null,
	last_error_code = $4,
	updated_at = $5
where id = $1 and status = 'in_flight' and lease_owner = $2
returning id, delegation_id, callback_run_id, destination, publication,
	channel_id, root_post_id, message, props, payload_sha256, external_id,
	status, attempt_count, coalesce(lease_owner, ''), lease_expires_at,
	last_attempt_at, last_error_code, mattermost_post_id, delivered_at,
	created_at, updated_at;
