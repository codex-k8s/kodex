-- name: avatar_upload_mark_materialized :one
UPDATE control_plane.agent_avatar_upload_reservations
SET object_version = @object_version,
    object_etag = @object_etag,
    state = 'MATERIALIZED',
    expires_at = clock_timestamp() + interval '15 minutes',
    version = version + 1,
    updated_at = clock_timestamp()
WHERE ref = @reservation_ref
  AND organization_id = @organization_id::uuid
  AND object_key = @object_key
  AND digest = @digest
  AND size_bytes = @size_bytes
  AND (
      state = 'RESERVED' OR
      (state = 'MATERIALIZED' AND object_version = @object_version AND object_etag = @object_etag)
  )
RETURNING version;
