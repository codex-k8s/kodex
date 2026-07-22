-- name: automation_owner_attention__claim :one
with candidate as materialized (
	select attention.id
	from matter_codex_owner_attention_requests attention
	where attention.request_kind = 'automation'
		and attention.status = 'open'
		and attention.mattermost_post_id = ''
		and attention.automation_delivery_next_attempt_at <= $5
		and (
			attention.automation_delivery_claim_token is null
			or attention.automation_delivery_lease_expires_at <= $3
		)
		and ($1::bigint = 0 or attention.automation_scheduled_run_id = $1)
	order by attention.id
	limit 1
	for update skip locked
)
update matter_codex_owner_attention_requests attention
set automation_delivery_claim_token = $2,
	automation_delivery_claimed_at = $3,
	automation_delivery_lease_expires_at = $4,
	automation_delivery_fence = attention.automation_delivery_fence + 1,
	updated_at = $3
from candidate,
	matter_codex_scheduled_runs run
where attention.id = candidate.id
	and run.id = attention.automation_scheduled_run_id
returning
	attention.id,
	attention.automation_scheduled_run_id,
	run.public_id,
	attention.process_run_id,
	attention.automation_policy_revision_id,
	attention.automation_root_initiator_user_id,
	attention.automation_mattermost_channel_id,
	attention.automation_mattermost_root_post_id,
	attention.mattermost_post_id,
	attention.status,
	attention.automation_delivery_id,
	attention.automation_delivery_message,
	attention.automation_delivery_props,
	attention.automation_delivery_payload_sha256,
	attention.automation_delivery_claim_token,
	attention.automation_delivery_claimed_at,
	attention.automation_delivery_lease_expires_at,
	attention.automation_delivery_confirmation_pending,
	attention.automation_delivery_fence;
