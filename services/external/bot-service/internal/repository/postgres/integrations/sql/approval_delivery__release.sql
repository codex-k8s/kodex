-- name: approval_delivery__release :exec
update matter_codex_approval_requests
set delivery_state = 'pending', delivery_lease_owner = '', delivery_lease_expires_at = null,
	delivery_last_reason = $3, updated_at = $4
where id = $1 and state = 'pending' and delivery_state = 'in_flight' and delivery_lease_owner = $2;
