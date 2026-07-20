-- name: agent_delegation_callback_deliveries__insert :one
insert into matter_codex_agent_delegation_callback_deliveries(
	delegation_id, callback_run_id, destination, publication,
	channel_id, root_post_id, message, props, payload_sha256, external_id
) values ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)
on conflict (delegation_id, callback_run_id, destination, publication) do nothing
returning id, delegation_id, callback_run_id, destination, publication,
	channel_id, root_post_id, message, props, payload_sha256, external_id,
	status, attempt_count, coalesce(lease_owner, ''), lease_expires_at,
	last_attempt_at, last_error_code, mattermost_post_id, delivered_at,
	created_at, updated_at;
