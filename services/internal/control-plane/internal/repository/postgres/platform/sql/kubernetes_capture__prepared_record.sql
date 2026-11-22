-- name: kubernetes_capture__prepared_record :exec
UPDATE control_plane.prepared_content SET state='STAGED',object_version=@object_version,object_etag=@object_etag,updated_at=clock_timestamp()
WHERE id=@id::uuid AND source_profile=@source_profile AND @source_profile IN('KUBERNETES_OUTPUT','KUBERNETES_FILE_SOURCE') AND request_digest=@request_digest AND generation=1
 AND(state='UNKNOWN' OR state='IN_FLIGHT' AND writer_deadline<=clock_timestamp());
