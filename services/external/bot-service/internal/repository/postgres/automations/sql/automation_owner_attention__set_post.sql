-- name: automation_owner_attention__set_post :exec
update matter_codex_owner_attention_requests
set mattermost_post_id = $6,
	updated_at = $7
where id = $1
	and automation_scheduled_run_id = $2
	and automation_delivery_id = $3
	and automation_mattermost_channel_id = $4
	and automation_mattermost_root_post_id = $5
	and status = 'open'
	and (mattermost_post_id = '' or mattermost_post_id = $6);
