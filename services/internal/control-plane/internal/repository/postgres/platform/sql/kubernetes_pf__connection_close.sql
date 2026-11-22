-- name: kubernetes_pf__connection_close :exec
UPDATE control_plane.kubernetes_port_forward_connections
SET state='CLOSED',closed_at=clock_timestamp(),reason_code=@reason_code,
 close_stage_id=nullif(@stage_id,'')::uuid
WHERE organization_id=@organization_id::uuid AND stream_id=@stream_id::uuid AND id=@connection_id::uuid
 AND state IN('PREPARED','OPENING','OPEN');
