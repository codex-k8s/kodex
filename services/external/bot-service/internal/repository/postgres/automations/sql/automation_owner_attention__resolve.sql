-- name: automation_owner_attention__resolve :exec
update matter_codex_owner_attention_requests
set status = 'resolved',
	resolved_at = $4,
	resolved_by_user_id = $2,
	resolved_by_post_id = $3,
	automation_resolved_by_post_create_at = $5,
	updated_at = $4
where id = $1
	and status = 'open'
	and automation_mattermost_post_create_at is not null
	and $5::bigint > automation_mattermost_post_create_at;
