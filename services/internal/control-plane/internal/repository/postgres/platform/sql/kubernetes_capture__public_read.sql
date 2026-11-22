-- name: kubernetes_capture__public_read :one
SELECT capture.source_snapshot,capture.snapshot_digest,capture.capture_generation,capture.version,
 capture.state,coalesce(capture.capture_kind,''),capture.reason_code,capture.attempt_count,
 capture.repair_generation,capture.retain_until,clock_timestamp()
FROM control_plane.kubernetes_output_captures capture
WHERE capture.id=@capture_id::uuid AND capture.organization_id=@organization_id::uuid
FOR UPDATE OF capture;
