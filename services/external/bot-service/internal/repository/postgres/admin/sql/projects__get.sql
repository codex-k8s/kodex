-- name: projects__get :one
select id, name, slug, mattermost_team_id, description, advanced_settings::text, created_at, updated_at
from matter_codex_projects
where id = $1;
