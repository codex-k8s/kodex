-- name: kubernetes_capture__loss_read :one
SELECT payload_digest,byte_count,reason_code FROM control_plane.kubernetes_capture_losses
WHERE organization_id=@organization_id::uuid AND stream_id=@stream_id::uuid
 AND source_ref=@source_ref AND source_generation=@source_generation AND native_ordinal=@native_ordinal;
