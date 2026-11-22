-- name: kubernetes_pf__connection_read :one
SELECT connection.id::text,connection.ref,connection.generation,connection.slot,connection.state,connection.reason_code,
 connection.local_write_closed_at IS NOT NULL,connection.read_end_observed_at IS NOT NULL,connection.native_joined_at IS NOT NULL,
 connection.open_key,connection.open_intent_digest,connection.open_expected_version,connection.open_stage_id::text,
 coalesce(connection.close_stage_id::text,''),coalesce(close_stage.submission_key,''),
 open_stage.native_admitted_at IS NOT NULL,open_stage.acknowledged_at IS NOT NULL
FROM control_plane.kubernetes_port_forward_connections connection
JOIN control_plane.kubernetes_input_stages open_stage ON open_stage.id=connection.open_stage_id AND open_stage.organization_id=connection.organization_id AND open_stage.stream_id=connection.stream_id
LEFT JOIN control_plane.kubernetes_input_stages close_stage ON close_stage.id=connection.close_stage_id AND close_stage.organization_id=connection.organization_id AND close_stage.stream_id=connection.stream_id
WHERE connection.stream_id=@stream_id::uuid AND connection.organization_id=@organization_id::uuid AND connection.actor_id=@actor_id::uuid
 AND((@connection_ref<>'' AND connection.ref=@connection_ref) OR(@open_key<>'' AND connection.open_key=@open_key))
FOR UPDATE OF connection;
