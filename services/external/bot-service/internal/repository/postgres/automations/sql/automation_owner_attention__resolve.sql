-- name: automation_owner_attention__resolve :exec
update matter_codex_owner_attention_requests
set status = 'resolved',
	resolved_at = $5,
	resolved_by_user_id = $2,
	resolved_by_post_id = $3,
	automation_response_post_create_at = $4,
	updated_at = $5
where id = $1
	and status = 'open';
