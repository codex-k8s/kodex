-- name: agent_delegation_callback_deliveries__list :many
select id, delegation_id, callback_run_id, destination, publication,
	channel_id, root_post_id, message, props, payload_sha256, external_id,
	status, attempt_count, coalesce(lease_owner, ''), lease_expires_at,
	last_attempt_at, last_error_code, mattermost_post_id, delivered_at,
	created_at, updated_at
from matter_codex_agent_delegation_callback_deliveries
where delegation_id = $1 and callback_run_id = $2
order by destination, publication, id;
