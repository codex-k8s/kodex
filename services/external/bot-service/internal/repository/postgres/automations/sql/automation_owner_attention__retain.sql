-- name: automation_owner_attention__retain :exec
update matter_codex_owner_attention_requests
set automation_delivery_confirmation_pending = true,
	automation_delivery_lease_expires_at = $6,
	updated_at = $7
where id = $1
	and request_kind = 'automation'
	and automation_scheduled_run_id = $2
	and automation_delivery_id = $3
	and automation_delivery_claim_token = $4
	and automation_delivery_fence = $5
	and status = 'open'
	and mattermost_post_id = '';
