-- name: personal_credential_test__lock_binding :one
SELECT id::text FROM control_plane.integration_personal_credentials
WHERE organization_id=@organization_id::uuid AND id=@binding_id::uuid FOR UPDATE
