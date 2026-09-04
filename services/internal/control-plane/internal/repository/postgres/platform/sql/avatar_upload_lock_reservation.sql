-- name: avatar_upload_lock_reservation :one
SELECT reservation.project_id::text, reservation.agent_id::text,
       reservation.artifact_ref, reservation.file_name, reservation.media_type,
       reservation.size_bytes, reservation.digest, reservation.object_key,
       reservation.object_version, reservation.object_etag, reservation.state,
       reservation.expected_agent_version, reservation.version
FROM control_plane.agent_avatar_upload_reservations reservation
WHERE reservation.ref = @reservation_ref
  AND reservation.organization_id = @organization_id::uuid
  AND reservation.actor_id = @actor_id::uuid
  AND reservation.operation = 'agent.avatar.upload'
  AND reservation.idempotency_key = @idempotency_key
  AND reservation.intent_digest = @intent_digest
FOR UPDATE;
