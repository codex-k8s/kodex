-- name: universal_rooms__upsert :one
insert into matter_codex_rooms(
	organization_scope, workspace_id, legacy_chat_id, name, slug, description,
	room_type, purpose, work_policy, mattermost_channel_id, status, managed_by,
	source_revision, record_version
)
select
	workspace.organization_scope, workspace.id, $1, $2, $3, $4,
	$5, $6, $7, $8, 'active', 'ui', 'legacy-chat:' || $1::text, 1
from matter_codex_workspaces workspace
where workspace.legacy_project_id = $9
on conflict (legacy_chat_id) do update set
	workspace_id = excluded.workspace_id,
	name = excluded.name,
	slug = excluded.slug,
	description = excluded.description,
	room_type = excluded.room_type,
	purpose = excluded.purpose,
	work_policy = excluded.work_policy,
	mattermost_channel_id = excluded.mattermost_channel_id,
	status = excluded.status,
	source_revision = excluded.source_revision,
	record_version = matter_codex_rooms.record_version + 1,
	updated_at = now()
where matter_codex_rooms.managed_by = 'ui'
returning id, organization_scope, workspace_id, legacy_chat_id, name, slug,
	description, room_type, purpose, work_policy, mattermost_channel_id, status,
	managed_by, source_revision, provenance::text, record_version, created_at, updated_at;
