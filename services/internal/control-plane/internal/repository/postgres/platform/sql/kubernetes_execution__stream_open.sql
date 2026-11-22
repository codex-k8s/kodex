-- name: kubernetes_execution__stream_open :exec
UPDATE control_plane.kubernetes_execution_streams
SET state='ACTIVE',handshake_digest=@handshake_digest,handshake_json=@handshake_json::jsonb
WHERE id=@stream_id::uuid AND organization_id=@organization_id::uuid AND state='PREPARED';
