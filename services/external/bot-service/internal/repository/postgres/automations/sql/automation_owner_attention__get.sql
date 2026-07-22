-- name: automation_owner_attention__get :one
select
	attention.id,
	attention.automation_scheduled_run_id,
	run.public_id,
	attention.process_run_id,
	attention.automation_policy_revision_id,
	attention.automation_root_initiator_user_id,
	attention.automation_mattermost_channel_id,
	attention.automation_mattermost_root_post_id,
	attention.mattermost_post_id,
	attention.automation_mattermost_post_create_at,
	attention.status,
	attention.automation_delivery_id,
	attention.automation_delivery_message,
	attention.automation_delivery_props,
	attention.automation_delivery_payload_sha256,
	attention.automation_delivery_claim_token,
	attention.automation_delivery_claimed_at,
	attention.automation_delivery_lease_expires_at,
	attention.automation_delivery_confirmation_pending,
	attention.automation_delivery_fence
from matter_codex_owner_attention_requests attention
join matter_codex_scheduled_runs run
	on run.id = attention.automation_scheduled_run_id
where attention.automation_scheduled_run_id = $1
	and attention.request_kind = 'automation';
