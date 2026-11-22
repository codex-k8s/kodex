-- name: ssh_setup__observation :one
INSERT INTO control_plane.ssh_identity_upload_observations(organization_id,setup_id,attempt_id,outcome,receipt_sha256,receipt_body,recorded_at)
VALUES(@organization_id::uuid,@setup_id::uuid,@attempt_id::uuid,@outcome,@receipt_digest,@receipt_body,clock_timestamp())
RETURNING recorded_at;
