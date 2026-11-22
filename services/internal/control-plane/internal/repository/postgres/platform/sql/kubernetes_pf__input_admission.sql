-- name: kubernetes_pf__input_admission :exec
UPDATE control_plane.kubernetes_port_forward_connections
SET state='OPENING' WHERE id=@connection_id::uuid AND organization_id=@organization_id::uuid AND stream_id=@stream_id::uuid AND state='PREPARED';
