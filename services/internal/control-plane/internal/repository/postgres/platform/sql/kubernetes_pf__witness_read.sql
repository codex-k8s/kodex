-- name: kubernetes_pf__witness_read :one
SELECT connection.id::text,connection.ref,connection.generation,connection.slot,connection.state,
 connection.local_write_closed_at IS NOT NULL,connection.open_stage_id::text,coalesce(connection.close_stage_id::text,''),
 open_stage.native_admitted_at IS NOT NULL
FROM control_plane.kubernetes_port_forward_connections connection
JOIN control_plane.kubernetes_input_stages open_stage ON open_stage.id=connection.open_stage_id AND open_stage.organization_id=connection.organization_id
WHERE connection.organization_id=@organization_id::uuid AND connection.stream_id=@stream_id::uuid AND connection.ref=@connection_ref AND connection.generation=@generation
FOR UPDATE OF connection;
