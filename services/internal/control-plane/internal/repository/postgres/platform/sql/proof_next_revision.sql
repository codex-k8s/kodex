-- name: proof_next_revision :one
UPDATE control_plane.installation
SET authority_proof_sequence = authority_proof_sequence + 1
WHERE singleton
RETURNING authority_proof_sequence
