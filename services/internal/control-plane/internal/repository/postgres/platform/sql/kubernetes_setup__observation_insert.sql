-- name: kubernetes_setup__observation_insert :exec
INSERT INTO control_plane.kubernetes_identity_upload_observations(attempt_id,outcome,identity_revision_id)
VALUES(@attempt_id::uuid,@outcome,nullif(@identity_id,'')::uuid)
ON CONFLICT(attempt_id,outcome) DO NOTHING;
