-- name: proof_next_revision :one
UPDATE control_plane.authority_proof_revision_counter
SET revision = revision + 1
WHERE singleton AND revision < 9007199254740991
RETURNING revision
