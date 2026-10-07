-- name: role_images_superseded_expiry_clock :exec
UPDATE control_plane.image_artifacts
SET admission_claim_expires_at=clock_timestamp()-interval '1 second'
WHERE ref=$1 AND version=$2 AND admission_fence=$3 AND admission_state='CLAIMED';
