-- name: approval_delivery__claim :one
update matter_codex_approval_requests
set delivery_state = 'in_flight', delivery_lease_owner = $2,
	delivery_lease_expires_at = $4, delivery_last_reason = '', updated_at = $3
where id = $1
	and state = 'pending'
	and mattermost_post_id = ''
	and (
		delivery_state = 'pending'
		or (delivery_state = 'in_flight' and delivery_lease_expires_at <= $3)
	)
returning id;
