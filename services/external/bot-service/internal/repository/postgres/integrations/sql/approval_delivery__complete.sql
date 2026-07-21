-- name: approval_delivery__complete :exec
update matter_codex_approval_requests
set mattermost_post_id = $3, delivery_state = 'delivered', delivery_lease_owner = '',
	delivery_lease_expires_at = null, delivery_last_reason = '', updated_at = $4
where id = $1 and state = 'pending' and delivery_state = 'in_flight'
	and delivery_lease_owner = $2 and mattermost_post_id = '';
