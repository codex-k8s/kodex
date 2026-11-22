-- name: kubernetes_capture_repair_claim :one
SELECT capture_ref,lease_ref,fence,generation,snapshot_digest,expires_at
FROM control_plane.kubernetes_capture_claim($1,1,60);
