-- name: kubernetes_capture__stream_lock :one
SELECT stream.id::text FROM control_plane.kubernetes_execution_streams stream
WHERE stream.id=@stream_id::uuid AND stream.organization_id=@organization_id::uuid
FOR UPDATE;
