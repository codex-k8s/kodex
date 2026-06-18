-- name: thread_contexts__upsert :one
with upserted as (
	insert into matter_codex_thread_contexts(
		project_id,
		chat_id,
		mattermost_channel_id,
		mattermost_root_post_id,
		repository_id,
		status,
		pending_mattermost_post_id,
		pending_user_id,
		pending_user_name,
		pending_message
	) values (
		$1,
		$2,
		$3,
		$4,
		nullif($5, 0),
		$6,
		$7,
		$8,
		$9,
		$10
	)
	on conflict (chat_id, mattermost_root_post_id) do update set
		project_id = excluded.project_id,
		mattermost_channel_id = excluded.mattermost_channel_id,
		repository_id = excluded.repository_id,
		status = excluded.status,
		pending_mattermost_post_id = case when excluded.pending_mattermost_post_id <> '' then excluded.pending_mattermost_post_id else matter_codex_thread_contexts.pending_mattermost_post_id end,
		pending_user_id = case when excluded.pending_user_id <> '' then excluded.pending_user_id else matter_codex_thread_contexts.pending_user_id end,
		pending_user_name = case when excluded.pending_user_name <> '' then excluded.pending_user_name else matter_codex_thread_contexts.pending_user_name end,
		pending_message = case when excluded.pending_message <> '' then excluded.pending_message else matter_codex_thread_contexts.pending_message end,
		updated_at = now()
	returning *, (xmax = 0) as created
)
select
	u.id,
	u.project_id,
	u.chat_id,
	u.mattermost_channel_id,
	u.mattermost_root_post_id,
	coalesce(u.repository_id, 0),
	coalesce(r.provider, ''),
	coalesce(r.owner, ''),
	coalesce(r.name, ''),
	coalesce(r.default_branch, ''),
	u.status,
	u.pending_mattermost_post_id,
	u.pending_user_id,
	u.pending_user_name,
	u.pending_message,
	u.created_at,
	u.updated_at,
	u.created
from upserted u
left join matter_codex_repositories r on r.id = u.repository_id;
