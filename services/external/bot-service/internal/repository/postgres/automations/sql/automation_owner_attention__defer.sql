-- name: automation_owner_attention__defer :exec
update matter_codex_owner_attention_requests
set automation_delivery_claim_token = null,
	automation_delivery_claimed_at = null,
	automation_delivery_lease_expires_at = null,
	automation_delivery_next_attempt_at = $6,
	updated_at = $7
where id = $1
	and request_kind = 'automation'
	and automation_scheduled_run_id = $2
	and automation_delivery_id = $3
	and automation_delivery_claim_token = $4
	and automation_delivery_fence = $5
	and status = 'open'
	and mattermost_post_id = '';
