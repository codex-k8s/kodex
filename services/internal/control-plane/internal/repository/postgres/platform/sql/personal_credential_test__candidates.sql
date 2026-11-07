-- name: personal_credential_test__candidates :many
SELECT ref FROM control_plane.integration_personal_credential_tests
WHERE organization_id=@organization_id::uuid
 AND (state='DUE' OR (state IN ('CLAIMED','PROBING') AND lease_expires_at<=clock_timestamp()))
ORDER BY created_at,ref LIMIT @limit
