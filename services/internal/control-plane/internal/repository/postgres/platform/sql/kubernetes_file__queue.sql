-- name: kubernetes_file__queue :exec
UPDATE control_plane.kubernetes_input_stages stage SET state='QUEUED',recorded_at=clock_timestamp()
WHERE stage.id=@stage_id::uuid AND stage.stream_id=@stream_id::uuid AND stage.organization_id=@organization_id::uuid
 AND stage.channel='FILE_CONTENT' AND stage.state='ATTEMPTING' AND stage.object_key IS NULL
 AND stage.object_version IS NULL AND stage.object_etag IS NULL
 AND EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_frames frame
  JOIN control_plane.kubernetes_file_input_sources source ON source.id=frame.source_id AND source.organization_id=frame.organization_id
  WHERE frame.stage_id=stage.id AND frame.stream_id=stage.stream_id AND frame.organization_id=stage.organization_id
   AND source.state='READY' AND source.payload_purged_at IS NULL AND source.authorized_until>clock_timestamp());
