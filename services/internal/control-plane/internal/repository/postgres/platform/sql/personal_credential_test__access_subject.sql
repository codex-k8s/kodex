-- name: personal_credential_test__access_subject :one
SELECT ref FROM control_plane.subjects
WHERE organization_id=@organization_id::uuid AND id=@subject_id::uuid AND kind='USER' AND active
