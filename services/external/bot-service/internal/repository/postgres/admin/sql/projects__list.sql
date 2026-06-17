-- name: projects__list :many
select id, name, slug, mattermost_team_id, description, advanced_settings::text, created_at, updated_at
from matter_codex_projects
order by updated_at desc, name
limit $1;
