-- name: kubernetes_file__prepared_link :exec
UPDATE control_plane.kubernetes_file_input_sources source SET prepared_content_id=@prepared_id::uuid
FROM control_plane.prepared_content content
WHERE source.ref=@source_ref AND source.organization_id=@organization_id::uuid AND source.state='PREPARING'
 AND source.writer_deadline>clock_timestamp() AND source.authorized_until>clock_timestamp()
 AND source.prepared_content_id IS NULL AND content.id=@prepared_id::uuid AND content.source_profile_ref=source.ref
 AND content.organization_id=source.organization_id AND content.source_profile='KUBERNETES_FILE_SOURCE';
