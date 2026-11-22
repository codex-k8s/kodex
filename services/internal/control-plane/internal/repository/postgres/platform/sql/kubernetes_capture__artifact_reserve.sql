-- name: kubernetes_capture__artifact_reserve :one
INSERT INTO control_plane.kubernetes_capture_artifact_heads(organization_id,stream_id,channel,artifact_ref)
VALUES(@organization_id::uuid,@stream_id::uuid,@channel,@artifact_ref)
ON CONFLICT(stream_id,channel) DO UPDATE SET artifact_ref=control_plane.kubernetes_capture_artifact_heads.artifact_ref
RETURNING artifact_ref,coalesce(artifact_id::text,'');
