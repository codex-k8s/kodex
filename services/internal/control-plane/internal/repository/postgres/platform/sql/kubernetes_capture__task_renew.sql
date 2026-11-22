-- name: kubernetes_capture__task_renew :one
UPDATE control_plane.kubernetes_capture_attempts attempt
SET expires_at=greatest(attempt.expires_at,least(clock_timestamp()+interval '120 seconds',attempt.hard_deadline))
FROM control_plane.kubernetes_output_captures capture
JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id AND stream.organization_id=capture.organization_id
WHERE capture.id=attempt.capture_id AND capture.organization_id=attempt.organization_id
 AND capture.ref=@capture_ref AND capture.snapshot_digest=@snapshot_digest
 AND capture.state='CLAIMED' AND capture.generation=attempt.generation AND stream.capture_generation=capture.capture_generation
 AND attempt.ref=@lease_ref AND attempt.generation=@generation AND attempt.workload_instance=@workload_instance AND attempt.fence_digest=@fence_digest
 AND attempt.state='ACTIVE' AND attempt.expires_at>clock_timestamp() AND attempt.hard_deadline>clock_timestamp()
RETURNING attempt.expires_at;
