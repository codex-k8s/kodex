-- name: kubernetes_pf__frame_context :one
SELECT stream.id::text,frame.channel FROM control_plane.kubernetes_execution_streams stream
JOIN control_plane.kubernetes_execution_frames frame ON frame.stream_id=stream.id AND frame.organization_id=stream.organization_id
WHERE stream.organization_id=@organization_id::uuid AND stream.ref=@stream_ref
 AND frame.direction=@direction AND frame.sequence=@sequence AND frame.payload_digest=@payload_digest;
