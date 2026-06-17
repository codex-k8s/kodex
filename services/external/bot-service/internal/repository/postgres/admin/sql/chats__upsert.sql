-- name: chats__upsert :one
insert into matter_codex_chats(
	project_id,
	mattermost_channel_id,
	name,
	slug,
	description,
	chat_type,
	root_github_issue,
	work_policy,
	settings
) values (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	coalesce(nullif($9, '')::jsonb, '{}'::jsonb)
)
on conflict (project_id, slug) do update set
	mattermost_channel_id = excluded.mattermost_channel_id,
	name = excluded.name,
	description = excluded.description,
	chat_type = excluded.chat_type,
	root_github_issue = excluded.root_github_issue,
	work_policy = excluded.work_policy,
	settings = excluded.settings,
	updated_at = now()
returning id, project_id, mattermost_channel_id, name, slug, description, chat_type, root_github_issue, work_policy, settings::text, created_at, updated_at, (xmax = 0) as created;
