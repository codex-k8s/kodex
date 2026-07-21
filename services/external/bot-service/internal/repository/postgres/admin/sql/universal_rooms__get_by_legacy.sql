-- name: universal_rooms__get_by_legacy :one
select id, organization_scope, workspace_id, legacy_chat_id, name, slug,
	description, room_type, purpose, work_policy, mattermost_channel_id, status,
	managed_by, source_revision, provenance::text, record_version, created_at, updated_at
from matter_codex_rooms
where legacy_chat_id = $1;
