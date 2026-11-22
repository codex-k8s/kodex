-- name: kubernetes_file__source_ready :exec
UPDATE control_plane.kubernetes_file_input_sources SET state='READY',reason_code='NONE',writer_completed_at=coalesce(writer_completed_at,clock_timestamp()),
 object_key=@object_key,object_version=@object_version,object_etag=@object_etag,authorized_until=@authorized_until,source_access_until=@source_access_until
WHERE id=@source_id::uuid AND organization_id=@organization_id::uuid AND state IN('PREPARING','UNKNOWN')
 AND reason_code<>'INPUT_UNKNOWN' AND prepared_content_id=@prepared_id::uuid AND payload_purged_at IS NULL;
