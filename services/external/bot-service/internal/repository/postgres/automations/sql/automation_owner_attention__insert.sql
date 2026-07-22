-- name: automation_owner_attention__insert :one
insert into matter_codex_owner_attention_requests (
	process_run_id,
	turn_id,
	severity,
	summary,
	options,
	recommendation,
	evidence_links,
	pause_scope,
	idempotency_key,
	status,
	automation_scheduled_run_id,
	automation_project_id,
	automation_policy_revision_id,
	automation_root_initiator_user_id,
	automation_mattermost_channel_id,
	automation_mattermost_root_post_id,
	automation_delivery_id,
	automation_delivery_message,
	automation_delivery_props,
	automation_delivery_payload_sha256
)
values (
	$1, $2, 'normal', $3, '[]'::jsonb, $4, '[]'::jsonb, 'turn', $5, 'open',
	$6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15
)
on conflict (automation_scheduled_run_id) do nothing
returning id;
