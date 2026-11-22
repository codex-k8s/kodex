-- name: kubernetes_input__ack :exec
UPDATE control_plane.kubernetes_input_stages stage SET state='ACKNOWLEDGED',acknowledged_at=clock_timestamp()
FROM control_plane.kubernetes_execution_streams stream
WHERE stage.stream_id=stream.id AND stage.organization_id=stream.organization_id AND stage.organization_id=@organization_id::uuid
 AND stream.ref=@stream_ref AND stage.sequence=@sequence AND stage.payload_digest=@payload_digest
 AND stage.native_admitted_at IS NOT NULL AND stage.state<>'ACKNOWLEDGED';
