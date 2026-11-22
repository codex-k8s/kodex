-- name: kubernetes_file__bind_stream :exec
UPDATE control_plane.kubernetes_file_input_sources source SET stream_id=stream.id
FROM control_plane.kubernetes_execution_streams stream
WHERE source.id=@source_id::uuid AND source.organization_id=@organization_id::uuid
 AND source.state='READY' AND source.stream_id IS NULL AND source.authorized_until>clock_timestamp()
 AND stream.ref=@stream_ref AND stream.organization_id=source.organization_id;
