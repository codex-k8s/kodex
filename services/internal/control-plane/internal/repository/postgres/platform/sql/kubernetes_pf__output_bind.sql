-- name: kubernetes_pf__output_bind :exec
INSERT INTO control_plane.kubernetes_port_forward_frames(organization_id,stream_id,direction,sequence,connection_id,action,native_ordinal,payload_digest,byte_count)
VALUES(@organization_id::uuid,@stream_id::uuid,'OUTPUT',@sequence,@connection_id::uuid,@action,@native_ordinal,@payload_digest,@byte_count);
