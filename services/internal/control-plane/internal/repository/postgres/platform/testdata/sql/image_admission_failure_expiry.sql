-- name: image_admission_failure_expiry :exec
UPDATE control_plane.image_artifacts
SET admission_claim_expires_at = clock_timestamp() - interval '1 second'
WHERE organization_id = $1::uuid AND ref = $2 AND admission_state = 'CLAIMED';
