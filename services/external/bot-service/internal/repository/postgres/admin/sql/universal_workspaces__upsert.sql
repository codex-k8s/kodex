-- name: universal_workspaces__upsert :one
insert into matter_codex_workspaces(
	organization_scope, legacy_project_id, name, slug, description,
	mattermost_team_id, status, managed_by, source_revision, record_version
) values (
	'installation', $1, $2, $3, $4, $5, 'active', 'ui', 'legacy-project:' || $1::text, 1
)
on conflict (legacy_project_id) do update set
	name = excluded.name,
	slug = excluded.slug,
	description = excluded.description,
	mattermost_team_id = excluded.mattermost_team_id,
	status = excluded.status,
	source_revision = excluded.source_revision,
	record_version = matter_codex_workspaces.record_version + 1,
	updated_at = now()
where matter_codex_workspaces.managed_by = 'ui'
returning id, organization_scope, legacy_project_id, name, slug, description,
	mattermost_team_id, status, managed_by, source_revision, provenance::text,
	record_version, created_at, updated_at;
