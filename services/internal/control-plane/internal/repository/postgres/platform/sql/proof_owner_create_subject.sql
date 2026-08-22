-- name: platform__proof_owner_create_subject :one
INSERT INTO control_plane.subjects
    (ref, organization_id, issuer, external_subject_digest, display_name, email_masked)
VALUES ($1, $2::uuid, 'verified-oidc-subject', $3, $4, $5)
RETURNING id::text
