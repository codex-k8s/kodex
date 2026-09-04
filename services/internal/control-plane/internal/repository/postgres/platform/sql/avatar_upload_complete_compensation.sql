-- name: avatar_upload_complete_compensation :exec
UPDATE control_plane.agent_avatar_upload_reservations reservation
SET state = 'COMPENSATED',
    version = reservation.version + 1,
    updated_at = clock_timestamp()
WHERE reservation.ref = @reservation_ref
  AND reservation.version = @expected_version
  AND reservation.state = 'COMPENSATING'
  AND reservation.object_key = @object_key
  AND reservation.object_version = @object_version
  AND reservation.object_etag = @object_etag
  AND reservation.digest = @digest
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.artifacts artifact
      JOIN control_plane.artifact_content content ON content.artifact_id = artifact.id
      WHERE artifact.ref = reservation.artifact_ref
        AND content.object_key = reservation.object_key
        AND content.digest = reservation.digest
  );
