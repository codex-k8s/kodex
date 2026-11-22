-- name: kubernetes_capture__task_read :one
SELECT capture.id::text,capture.organization_id::text,capture.stream_id::text,attempt.id::text,
 capture.source_snapshot,capture.snapshot_digest,capture.capture_generation,capture.stream_generation,
 capture.state,coalesce(capture.capture_kind,''),capture.reason_code,attempt.state,attempt.expires_at,attempt.hard_deadline,clock_timestamp()
FROM control_plane.kubernetes_output_captures capture
JOIN control_plane.kubernetes_capture_attempts attempt ON attempt.capture_id=capture.id AND attempt.organization_id=capture.organization_id
WHERE capture.ref=@capture_ref AND capture.snapshot_digest=@snapshot_digest
 AND attempt.ref=@lease_ref AND attempt.generation=@generation AND attempt.workload_instance=@workload_instance
 AND attempt.fence_digest=@fence_digest;
