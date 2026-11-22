-- name: kubernetes_output__credit :one
SELECT frame.grant_expires_at
FROM control_plane.kubernetes_execution_frames frame
WHERE frame.stream_id=@stream_id::uuid AND frame.organization_id=@organization_id::uuid
 AND frame.direction='OUTPUT' AND frame.sequence=@sequence AND frame.delivery_credits=@delivery_ordinal
 AND frame.channel=@channel AND frame.payload_digest=@payload_digest AND frame.byte_count=@byte_count
 AND frame.acknowledged_at IS NULL AND frame.grant_expires_at>clock_timestamp()
FOR UPDATE;
