-- name: kubernetes_file__frame_binding :one
SELECT source.ref,source.state,source.authorized_until,source.source_access_until,source.payload_purged_at,
 frame.ordinal,frame.byte_offset,frame.byte_count,frame.payload_digest,frame.end_of_input,
 source.object_key,source.object_version,source.object_etag,source.payload_digest,source.byte_count
FROM control_plane.kubernetes_file_input_frames frame
JOIN control_plane.kubernetes_file_input_sources source ON source.id=frame.source_id AND source.organization_id=frame.organization_id
WHERE frame.stage_id=@stage_id::uuid AND frame.organization_id=@organization_id::uuid AND frame.stream_id=@stream_id::uuid;
