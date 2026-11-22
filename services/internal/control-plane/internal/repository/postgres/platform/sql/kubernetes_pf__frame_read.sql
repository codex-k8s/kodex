-- name: kubernetes_pf__frame_read :one
SELECT connection.id::text,connection.ref,connection.generation,connection.slot,connection.state,
 connection.local_write_closed_at IS NOT NULL,connection.open_stage_id::text,coalesce(connection.close_stage_id::text,''),
 open_stage.native_admitted_at IS NOT NULL,binding.action,binding.native_ordinal,binding.payload_digest,binding.byte_count,coalesce(binding.input_stage_id::text,'')
FROM control_plane.kubernetes_port_forward_frames binding
JOIN control_plane.kubernetes_port_forward_connections connection ON connection.id=binding.connection_id AND connection.organization_id=binding.organization_id AND connection.stream_id=binding.stream_id
JOIN control_plane.kubernetes_input_stages open_stage ON open_stage.id=connection.open_stage_id AND open_stage.organization_id=connection.organization_id
WHERE binding.organization_id=@organization_id::uuid AND binding.stream_id=@stream_id::uuid AND binding.direction=@direction AND binding.sequence=@sequence
FOR UPDATE OF connection;
