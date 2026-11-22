-- name: kubernetes_capture__artifact_pin :exec
UPDATE control_plane.kubernetes_capture_artifact_heads
SET artifact_id=@artifact_id::uuid
WHERE organization_id=@organization_id::uuid AND stream_id=@stream_id::uuid AND channel=@channel
 AND artifact_ref=@artifact_ref AND (artifact_id IS NULL OR artifact_id=@artifact_id::uuid);
