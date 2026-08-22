-- name: platform__proof_next_revision :one
UPDATE control_plane.installation
SET platform_sequence = platform_sequence + 1
WHERE singleton
RETURNING platform_sequence
