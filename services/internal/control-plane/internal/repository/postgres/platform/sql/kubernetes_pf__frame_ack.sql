-- name: kubernetes_pf__frame_ack :exec
UPDATE control_plane.kubernetes_port_forward_connections connection SET
 state=CASE WHEN @direction='INPUT' AND @action='OPEN' AND connection.state='OPENING' THEN 'OPEN'
  WHEN @action='CLOSE' AND connection.state IN('PREPARED','OPENING','OPEN') THEN 'CLOSED' ELSE connection.state END,
 closed_at=CASE WHEN @action='CLOSE' THEN coalesce(connection.closed_at,clock_timestamp()) ELSE connection.closed_at END,
 local_write_closed_at=CASE WHEN @direction='INPUT' AND @action IN('HALF_CLOSE','CLOSE') THEN coalesce(connection.local_write_closed_at,clock_timestamp()) ELSE connection.local_write_closed_at END,
 read_end_observed_at=CASE WHEN @direction='OUTPUT' AND @action IN('HALF_CLOSE','CLOSE') THEN coalesce(connection.read_end_observed_at,clock_timestamp()) ELSE connection.read_end_observed_at END,
 native_joined_at=CASE WHEN @action='CLOSE' THEN coalesce(connection.native_joined_at,clock_timestamp()) ELSE connection.native_joined_at END
WHERE id=@connection_id::uuid AND organization_id=@organization_id::uuid AND stream_id=@stream_id::uuid;
