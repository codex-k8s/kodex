-- name: platform__proof_owner_find_subject :one
SELECT id::text
FROM control_plane.subjects
WHERE organization_id = $1::uuid AND external_subject_digest = $2 AND active
