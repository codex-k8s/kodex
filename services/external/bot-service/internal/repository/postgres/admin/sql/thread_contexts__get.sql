-- name: thread_contexts__get :one
select
	tc.id,
	tc.project_id,
	tc.chat_id,
	tc.mattermost_channel_id,
	tc.mattermost_root_post_id,
	coalesce(tc.repository_id, 0),
	coalesce(r.provider, ''),
	coalesce(r.owner, ''),
	coalesce(r.name, ''),
	coalesce(r.default_branch, ''),
	tc.status,
	tc.pending_mattermost_post_id,
	tc.pending_user_id,
	tc.pending_user_name,
	tc.pending_message,
	tc.pending_mattermost_file_ids,
	tc.created_at,
	tc.updated_at
from matter_codex_thread_contexts tc
left join matter_codex_repositories r on r.id = tc.repository_id
where tc.chat_id = $1 and tc.mattermost_root_post_id = $2;
