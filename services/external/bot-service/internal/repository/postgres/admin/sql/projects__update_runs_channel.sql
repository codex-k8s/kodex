-- name: projects__update_runs_channel :one
update matter_codex_projects
set mattermost_runs_channel_id = $2,
	updated_at = now()
where id = $1
returning id, name, slug, mattermost_team_id, mattermost_runs_channel_id, github_account_name, github_owner, github_owner_type, description, advanced_settings::text, created_at, updated_at;
