-- name: avatar_upload_mark_finalized :one
UPDATE control_plane.agent_avatar_upload_reservations
SET state = 'FINALIZED',
    finalized_at = clock_timestamp(),
    version = version + 1,
    updated_at = clock_timestamp()
WHERE ref = @reservation_ref
  AND version = @expected_reservation_version
  AND state = 'MATERIALIZED'
  AND artifact_ref = @artifact_ref
  AND object_key = @object_key
  AND object_version = @object_version
  AND object_etag = @object_etag
RETURNING version;
