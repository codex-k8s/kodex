-- name: avatar_upload_claim_compensation :one
UPDATE control_plane.agent_avatar_upload_reservations reservation
SET object_version = @object_version,
    object_etag = @object_etag,
    state = 'COMPENSATING',
    cleanup_claimed_at = clock_timestamp(),
    version = reservation.version + 1,
    updated_at = clock_timestamp()
WHERE reservation.ref = @reservation_ref
  AND reservation.organization_id = @organization_id::uuid
  AND reservation.object_key = @object_key
  AND reservation.digest = @digest
  AND reservation.size_bytes = @size_bytes
  AND reservation.state IN ('RESERVED', 'MATERIALIZED', 'COMPENSATING')
  AND (reservation.state = 'RESERVED' OR
       (reservation.object_version = @object_version AND reservation.object_etag = @object_etag))
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.artifacts artifact
      JOIN control_plane.artifact_content content ON content.artifact_id = artifact.id
      WHERE artifact.ref = reservation.artifact_ref
        AND content.object_key = @object_key
        AND content.digest = @digest
  )
RETURNING reservation.ref, reservation.object_key, reservation.object_version,
          reservation.object_etag, reservation.digest, reservation.size_bytes,
          reservation.version;
