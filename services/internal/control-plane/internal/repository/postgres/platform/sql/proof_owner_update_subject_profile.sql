-- name: proof_owner_update_subject_profile :exec
UPDATE control_plane.subjects
SET display_name = $3,
    email_masked = $4,
    updated_at = clock_timestamp()
WHERE organization_id = $1::uuid
  AND id = $2::uuid
  AND issuer = 'verified-oidc-subject'
  AND (display_name IS DISTINCT FROM $3 OR email_masked IS DISTINCT FROM $4);
