-- name: prepared_content_stage :exec
UPDATE control_plane.prepared_content
SET state=@state,object_version=@object_version,object_etag=@object_etag,
 writer_deadline=clock_timestamp(),updated_at=clock_timestamp()
WHERE id=@id AND state='IN_FLIGHT' AND generation=1;
