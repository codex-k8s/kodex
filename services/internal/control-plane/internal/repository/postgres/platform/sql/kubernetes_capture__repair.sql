-- name: kubernetes_capture__repair :one
UPDATE control_plane.kubernetes_output_captures capture
SET state='READY',repair_generation=repair_generation+1,repair_base_attempt_count=attempt_count,
 next_attempt_at=clock_timestamp(),reason_code=''
WHERE capture.id=@capture_id::uuid AND capture.organization_id=@organization_id::uuid
 AND capture.version=@expected_version AND capture.state='WAITING_OWNER'
 AND capture.version<9223372036854775807 AND capture.repair_generation<9223372036854775807
 AND capture.attempt_count<9223372036854775807 AND capture.generation<9223372036854775807
 AND (capture.retain_until IS NULL OR capture.retain_until>clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM control_plane.kubernetes_capture_attempts attempt
  WHERE attempt.capture_id=capture.id AND attempt.organization_id=capture.organization_id AND attempt.state='ACTIVE')
 AND EXISTS(SELECT 1 FROM control_plane.kubernetes_execution_streams stream WHERE stream.id=capture.stream_id
  AND stream.organization_id=capture.organization_id AND stream.capture_generation=capture.capture_generation)
RETURNING capture.repair_generation,capture.version,capture.attempt_count,clock_timestamp();
