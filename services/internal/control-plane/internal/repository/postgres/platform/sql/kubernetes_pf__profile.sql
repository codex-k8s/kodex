-- name: kubernetes_pf__profile :one
SELECT coalesce(stream.handshake_json->>'protocol',''),stream.pf_connection_generation,
 (SELECT count(*) FROM control_plane.kubernetes_port_forward_connections active
  WHERE active.stream_id=stream.id AND active.organization_id=stream.organization_id AND active.state IN('PREPARED','OPENING','OPEN'))
FROM control_plane.kubernetes_execution_streams stream
WHERE stream.id=@stream_id::uuid AND stream.organization_id=@organization_id::uuid;
