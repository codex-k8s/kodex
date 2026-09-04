-- name: avatar_upload_claim_expired :many
WITH candidate AS (
    SELECT reservation.id
    FROM control_plane.agent_avatar_upload_reservations reservation
    WHERE reservation.expires_at <= clock_timestamp()
      AND (
          reservation.state IN ('RESERVED', 'MATERIALIZED') OR
          (reservation.state = 'COMPENSATING' AND
           reservation.cleanup_claimed_at < clock_timestamp() - interval '5 minutes')
      )
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.artifacts artifact
          JOIN control_plane.artifact_content content ON content.artifact_id = artifact.id
          WHERE artifact.ref = reservation.artifact_ref
            AND content.object_key = reservation.object_key
            AND content.digest = reservation.digest
      )
    ORDER BY reservation.expires_at, reservation.ref
    FOR UPDATE SKIP LOCKED
    LIMIT @limit
)
UPDATE control_plane.agent_avatar_upload_reservations reservation
SET state = 'COMPENSATING',
    cleanup_claimed_at = clock_timestamp(),
    version = reservation.version + 1,
    updated_at = clock_timestamp()
FROM candidate
WHERE reservation.id = candidate.id
RETURNING reservation.ref, reservation.object_key, reservation.object_version,
          reservation.object_etag, reservation.digest, reservation.size_bytes,
          reservation.version;
