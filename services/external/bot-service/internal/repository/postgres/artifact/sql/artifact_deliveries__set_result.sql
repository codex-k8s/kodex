-- name: artifact_deliveries__set_result :one
update matter_codex_artifact_deliveries
set state = $2,
	mattermost_file_id = case when $3 = '' then mattermost_file_id else $3 end,
	mattermost_post_id = case when $4 = '' then mattermost_post_id else $4 end,
	error_code = $5,
	attempts = attempts + 1,
	updated_at = now()
where id = $1 and state <> 'delivered' and state <> 'quarantined'
returning id;
