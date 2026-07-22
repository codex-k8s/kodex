-- name: artifact_deliveries__find :one
select
	delivery.id, delivery.idempotency_key, delivery.bot_token_secret_ref, delivery.state,
	delivery.mattermost_file_id, delivery.mattermost_post_id, delivery.error_code, delivery.attempts,
	a.id, version.id, a.project_id, a.chat_id, a.session_id, delivery.role_id, delivery.runtime_turn_id, delivery.turn_id, a.direction,
	version.state, version.error_code, version.storage_key, version.original_name, version.safe_name,
	version.media_type, version.declared_media_type, version.size_bytes, version.sha256,
	a.mattermost_post_id, a.mattermost_file_id, 1,
	a.retention_until, version.created_at
from matter_codex_artifact_deliveries delivery
join matter_codex_artifact_versions version on version.id = delivery.artifact_version_id
join matter_codex_artifacts a on a.id = version.artifact_id
where delivery.project_id = $1 and delivery.chat_id = $2 and delivery.session_id = $3 and delivery.role_id = $4
	and a.project_id = $1 and a.chat_id = $2 and a.session_id = $3 and a.role_id = $4
	and delivery.runtime_turn_id = $5 and delivery.turn_id = $6 and delivery.idempotency_key = $7;
