-- name: kubernetes_execution__stream_close :exec
WITH expire_frames AS(UPDATE control_plane.kubernetes_execution_frames
 SET grant_expires_at=least(grant_expires_at,clock_timestamp())
 WHERE stream_id=@stream_id::uuid AND organization_id=@organization_id::uuid)
UPDATE control_plane.kubernetes_execution_streams
SET state=@state,closed_at=coalesce(closed_at,clock_timestamp()),terminal_digest=@terminal_digest,
 terminal_json=@terminal_json::jsonb,remote_outcome=@remote_outcome,
 handshake_digest=coalesce(handshake_digest,nullif(@observed_handshake_digest,'')),
 handshake_json=coalesce(handshake_json,nullif(@observed_handshake_json,'')::jsonb)
WHERE id=@stream_id::uuid AND organization_id=@organization_id::uuid AND terminal_digest IS NULL;
