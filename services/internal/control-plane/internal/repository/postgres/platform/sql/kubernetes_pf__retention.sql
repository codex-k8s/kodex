-- name: kubernetes_pf__retention :many
-- Stream lock принадлежит existing frame admission; pending CLOSE нельзя забыть из-за CLOSED owner state.
SELECT connection.ref,connection.generation,connection.slot,
 coalesce(close_stage.state IN('ATTEMPTING','QUEUED','CLAIMED','UNKNOWN'),false)
FROM control_plane.kubernetes_port_forward_connections connection
LEFT JOIN control_plane.kubernetes_input_stages close_stage
 ON close_stage.id=connection.close_stage_id AND close_stage.stream_id=connection.stream_id AND close_stage.organization_id=connection.organization_id
WHERE connection.organization_id=@organization_id::uuid AND connection.stream_id=@stream_id::uuid
 AND(connection.state IN('PREPARED','OPENING','OPEN') OR close_stage.state IN('ATTEMPTING','QUEUED','CLAIMED','UNKNOWN'))
ORDER BY connection.generation LIMIT 17;
