-- name: automation_owner_attention__list_pending :many
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
	attention.status,
	attention.automation_delivery_id,
	attention.automation_delivery_message,
	attention.automation_delivery_props,
	attention.automation_delivery_payload_sha256
from matter_codex_owner_attention_requests attention
join matter_codex_scheduled_runs run
	on run.id = attention.automation_scheduled_run_id
where attention.automation_scheduled_run_id is not null
	and attention.status = 'open'
	and attention.mattermost_post_id = ''
order by attention.id
limit $1;
