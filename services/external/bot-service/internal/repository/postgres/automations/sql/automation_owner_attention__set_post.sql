-- name: automation_owner_attention__set_post :exec
update matter_codex_owner_attention_requests
set mattermost_post_id = $6,
	automation_mattermost_post_create_at = $7,
	automation_delivery_claim_token = null,
	automation_delivery_claimed_at = null,
	automation_delivery_lease_expires_at = null,
	automation_delivery_confirmation_pending = false,
	updated_at = $10
where id = $1
	and request_kind = 'automation'
	and automation_scheduled_run_id = $2
	and automation_delivery_id = $3
	and automation_mattermost_channel_id = $4
	and automation_mattermost_root_post_id = $5
	and automation_delivery_claim_token = $8
	and automation_delivery_fence = $9
	and automation_delivery_confirmation_pending
	and status = 'open'
	and mattermost_post_id = '';
