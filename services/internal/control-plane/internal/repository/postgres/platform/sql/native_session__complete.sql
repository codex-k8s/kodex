-- name: native_session__complete :exec
WITH previous AS (
 SELECT session_id,current_archive_id FROM control_plane.session_storage
 WHERE organization_id=@organization_id::uuid AND native_writer_lease_ref=@lease_ref
   AND native_writer_revision_id=@runtime_revision_id::uuid
 FOR UPDATE
), superseded AS (
 UPDATE control_plane.session_archives archive
 SET lifecycle_state='SUPERSEDED',retention_until=GREATEST(retention_until,clock_timestamp()+interval '30 days')
 FROM previous WHERE archive.id=previous.current_archive_id AND archive.lifecycle_state='AVAILABLE'
 RETURNING archive.id
)
UPDATE control_plane.session_storage storage
SET native_session_id=@native_session_id,native_checkpoint=@native_checkpoint::jsonb,
 content_generation=@content_generation,state='LIVE',source_relative_path=@source_relative_path,
 source_sha256=@source_sha256,source_size_bytes=@source_size_bytes,current_archive_id=NULL,
 runtime_revision_id=@runtime_revision_id::uuid,native_writer_lease_ref=NULL,native_writer_revision_id=NULL,
 idle_since=clock_timestamp(),updated_at=clock_timestamp(),version=storage.version+1
FROM previous
WHERE storage.session_id=previous.session_id AND storage.organization_id=@organization_id::uuid
 AND storage.native_writer_lease_ref=@lease_ref AND storage.native_writer_revision_id=@runtime_revision_id::uuid;
