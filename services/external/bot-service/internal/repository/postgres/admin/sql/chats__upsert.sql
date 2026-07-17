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
	settings,
	system_purpose
) values (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	coalesce(nullif($9, '')::jsonb, '{}'::jsonb),
	coalesce(nullif($10, ''), 'custom')
)
on conflict (project_id, slug) do update set
	mattermost_channel_id = excluded.mattermost_channel_id,
	name = excluded.name,
	description = excluded.description,
	chat_type = excluded.chat_type,
	root_github_issue = excluded.root_github_issue,
	work_policy = excluded.work_policy,
	settings = excluded.settings,
	system_purpose = excluded.system_purpose,
	updated_at = now()
returning id, project_id, mattermost_channel_id, name, slug, description, chat_type, root_github_issue, work_policy, settings::text, system_purpose, created_at, updated_at, (xmax = 0) as created;
