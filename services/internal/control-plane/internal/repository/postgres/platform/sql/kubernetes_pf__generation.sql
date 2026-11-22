-- name: kubernetes_pf__generation :one
UPDATE control_plane.kubernetes_execution_streams SET pf_connection_generation=pf_connection_generation+1
WHERE id=@stream_id::uuid AND organization_id=@organization_id::uuid AND state='ACTIVE' AND pf_connection_generation<9223372036854775807
RETURNING pf_connection_generation;
