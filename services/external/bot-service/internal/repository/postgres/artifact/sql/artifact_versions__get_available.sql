-- name: artifact_versions__get_available :one
select
	a.id, version.id, a.project_id, a.chat_id, a.session_id, binding.role_id, binding.runtime_turn_id, binding.turn_id, a.direction,
	version.state, version.error_code, version.storage_key, version.original_name, version.safe_name,
	version.media_type, version.declared_media_type, version.size_bytes, version.sha256,
	a.mattermost_post_id, a.mattermost_file_id, binding.ordinal,
	a.retention_until, version.created_at
from matter_codex_message_artifact_bindings binding
join matter_codex_artifact_versions version on version.id = binding.artifact_version_id
join matter_codex_artifacts a on a.id = version.artifact_id
where binding.project_id = $1 and binding.chat_id = $2 and binding.session_id = $3 and binding.role_id = $4
	and binding.runtime_turn_id = $5 and binding.turn_id = $6
	and a.project_id = $1 and a.chat_id = $2 and a.session_id = $3 and a.role_id = $4
	and version.id = $7 and version.state = 'available';
