-- name: kubernetes_capture_repair_abort :exec
UPDATE control_plane.kubernetes_capture_attempts attempt
SET state='UNKNOWN',completed_at=clock_timestamp()
FROM control_plane.kubernetes_output_captures capture
WHERE capture.id=attempt.capture_id AND capture.organization_id=attempt.organization_id
 AND capture.ref=$1 AND attempt.ref=$2 AND attempt.generation=$3 AND attempt.state='ACTIVE'
 AND capture.generation=attempt.generation AND capture.state='CLAIMED';
