-- name: universal_workspaces__get_by_legacy :one
select id, organization_scope, legacy_project_id, name, slug, description,
	mattermost_team_id, status, managed_by, source_revision, provenance::text,
	record_version, created_at, updated_at
from matter_codex_workspaces
where legacy_project_id = $1;
