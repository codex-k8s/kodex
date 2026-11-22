-- name: kubernetes_input__stream_lock :exec
SELECT stream.id FROM control_plane.kubernetes_execution_streams stream
WHERE stream.organization_id=@organization_id::uuid AND stream.id=@stream_id::uuid FOR UPDATE;
