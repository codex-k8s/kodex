-- name: platform__proof_owner_create_subject :one
INSERT INTO control_plane.subjects
    (ref, organization_id, issuer, external_subject_digest, display_name)
VALUES ($1, $2::uuid, 'verified-oidc-subject', $3, 'Владелец')
RETURNING id::text
