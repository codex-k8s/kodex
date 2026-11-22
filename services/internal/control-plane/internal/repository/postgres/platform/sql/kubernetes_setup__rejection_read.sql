-- name: kubernetes_setup__rejection_read :one
SELECT response_sha256,truncated,coalesce(observed_uid::text,'')
FROM control_plane.kubernetes_identity_acknowledgement_rejections WHERE attempt_id=@attempt_id::uuid;
