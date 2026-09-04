-- name: avatar_upload_reserve :one
INSERT INTO control_plane.agent_avatar_upload_reservations (
    ref, organization_id, project_id, agent_id, actor_id, operation,
    idempotency_key, intent_digest, expected_agent_version, artifact_ref,
    file_name, media_type, size_bytes, digest, object_key, state, expires_at
)
VALUES (
    @reservation_ref, @organization_id::uuid, @project_id::uuid,
    @agent_id::uuid, @actor_id::uuid, 'agent.avatar.upload',
    @idempotency_key, @intent_digest, @expected_agent_version, @artifact_ref,
    @file_name, @media_type, @size_bytes, @digest, @object_key,
    'RESERVED', clock_timestamp() + interval '15 minutes'
)
ON CONFLICT (organization_id, actor_id, operation, idempotency_key)
DO UPDATE SET updated_at = control_plane.agent_avatar_upload_reservations.updated_at
WHERE control_plane.agent_avatar_upload_reservations.intent_digest = EXCLUDED.intent_digest
  AND control_plane.agent_avatar_upload_reservations.project_id = EXCLUDED.project_id
  AND control_plane.agent_avatar_upload_reservations.agent_id = EXCLUDED.agent_id
  AND control_plane.agent_avatar_upload_reservations.expected_agent_version = EXCLUDED.expected_agent_version
  AND control_plane.agent_avatar_upload_reservations.file_name = EXCLUDED.file_name
  AND control_plane.agent_avatar_upload_reservations.media_type = EXCLUDED.media_type
  AND control_plane.agent_avatar_upload_reservations.size_bytes = EXCLUDED.size_bytes
  AND control_plane.agent_avatar_upload_reservations.digest = EXCLUDED.digest
RETURNING ref, artifact_ref, object_key, object_version, object_etag, state, version;
