-- name: kubernetes_capture__reject :exec
WITH rejected AS(
 UPDATE control_plane.kubernetes_output_captures capture SET state='REJECTED',reason_code='CAPTURE_CONTENT_REJECTED'
 FROM control_plane.kubernetes_capture_attempts attempt,control_plane.kubernetes_execution_streams stream
 WHERE capture.ref=@capture_ref AND capture.snapshot_digest=@snapshot_digest AND capture.state='CLAIMED'
  AND attempt.capture_id=capture.id AND attempt.organization_id=capture.organization_id
  AND capture.generation=attempt.generation AND stream.id=capture.stream_id AND stream.organization_id=capture.organization_id
  AND stream.capture_generation=capture.capture_generation AND attempt.ref=@lease_ref AND attempt.generation=@generation
  AND attempt.workload_instance=@workload_instance AND attempt.fence_digest=@fence_digest AND attempt.state='ACTIVE'
  AND attempt.expires_at>clock_timestamp() AND attempt.hard_deadline>clock_timestamp()
 RETURNING attempt.id)
UPDATE control_plane.kubernetes_capture_attempts attempt SET state='COMPLETE',completed_at=clock_timestamp()
FROM rejected WHERE attempt.id=rejected.id;
