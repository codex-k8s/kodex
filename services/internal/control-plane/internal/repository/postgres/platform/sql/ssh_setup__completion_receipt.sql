-- name: ssh_setup__completion_receipt :one
SELECT receipt_body FROM control_plane.ssh_identity_upload_observations
WHERE attempt_id=@attempt_id::uuid AND outcome=@outcome AND receipt_sha256=@receipt_digest;
