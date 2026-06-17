-- name: projects__upsert :one
insert into matter_codex_projects(
	name,
	slug,
	mattermost_team_id,
	description,
	advanced_settings
) values (
	$1,
	$2,
	$3,
	$4,
	coalesce(nullif($5, '')::jsonb, '{}'::jsonb)
)
on conflict (slug) do update set
	name = excluded.name,
	mattermost_team_id = excluded.mattermost_team_id,
	description = excluded.description,
	advanced_settings = excluded.advanced_settings,
	updated_at = now()
returning id, name, slug, mattermost_team_id, description, advanced_settings::text, created_at, updated_at, (xmax = 0) as created;
