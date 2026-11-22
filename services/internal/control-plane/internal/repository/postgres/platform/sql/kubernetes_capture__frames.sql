-- name: kubernetes_capture__frames :many
SELECT frame.sequence,frame.channel,frame.payload_digest,frame.byte_count,frame.end_of_input,
 coalesce(frame.acknowledged_at<=@sealed_at,false),coalesce(stage.ref,''),coalesce(stage.object_key,''),
 coalesce(stage.object_version,''),coalesce(stage.object_etag,''),
 coalesce(stage.state='ACCEPTED' AND stage.channel=frame.channel AND stage.payload_digest=frame.payload_digest
  AND stage.byte_count=frame.byte_count AND stage.accepted_at<=frame.acknowledged_at
  AND stage.delivery_ordinal<=frame.delivery_credits,false)
FROM control_plane.kubernetes_execution_frames frame
LEFT JOIN control_plane.kubernetes_output_stages stage ON stage.stream_id=frame.stream_id
 AND stage.organization_id=frame.organization_id AND stage.sequence=frame.sequence
WHERE frame.organization_id=@organization_id::uuid AND frame.stream_id=@stream_id::uuid
 AND frame.direction='OUTPUT' AND frame.sequence>@after_sequence AND frame.sequence<=@watermark
ORDER BY frame.sequence LIMIT 256;
