-- name: kubernetes_capture__loss_insert :exec
INSERT INTO control_plane.kubernetes_capture_losses
 (organization_id,stream_id,source_ref,source_generation,native_ordinal,payload_digest,byte_count,reason_code)
VALUES(@organization_id::uuid,@stream_id::uuid,@source_ref,@source_generation,@native_ordinal,@payload_digest,@byte_count,@reason_code);
