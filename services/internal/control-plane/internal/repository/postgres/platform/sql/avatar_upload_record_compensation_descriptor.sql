-- name: avatar_upload_record_compensation_descriptor :one
UPDATE control_plane.agent_avatar_upload_reservations reservation
SET object_version = @object_version,
    object_etag = @object_etag,
    version = reservation.version + 1,
    updated_at = clock_timestamp()
WHERE reservation.ref = @reservation_ref
  AND reservation.version = @expected_version
  AND reservation.state = 'COMPENSATING'
  AND reservation.object_key = @object_key
  AND reservation.object_version = ''
  AND reservation.object_etag = ''
  AND reservation.digest = @digest
  AND reservation.size_bytes = @size_bytes
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.artifacts artifact
      JOIN control_plane.artifact_content content ON content.artifact_id = artifact.id
      WHERE artifact.ref = reservation.artifact_ref
        AND content.object_key = @object_key
        AND content.digest = @digest
  )
RETURNING reservation.version;
