-- name: kubernetes_pf__input_bind :exec
INSERT INTO control_plane.kubernetes_port_forward_frames(organization_id,stream_id,direction,sequence,connection_id,action,native_ordinal,payload_digest,byte_count,input_stage_id)
VALUES(@organization_id::uuid,@stream_id::uuid,'INPUT',@sequence,@connection_id::uuid,@action,0,@payload_digest,@byte_count,@stage_id::uuid);
