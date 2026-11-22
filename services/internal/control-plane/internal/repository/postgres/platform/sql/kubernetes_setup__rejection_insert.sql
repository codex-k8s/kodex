-- name: kubernetes_setup__rejection_insert :exec
INSERT INTO control_plane.kubernetes_identity_acknowledgement_rejections(attempt_id,response_sha256,truncated,observed_uid)
VALUES(@attempt_id::uuid,@response_digest,@truncated,nullif(@observed_uid,'')::uuid)
ON CONFLICT(attempt_id) DO NOTHING;
