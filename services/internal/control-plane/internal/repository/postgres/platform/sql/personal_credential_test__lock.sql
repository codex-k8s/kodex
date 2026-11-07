-- name: personal_credential_test__lock :one
SELECT id::text,input_snapshot,state,version,generation,COALESCE(lease_ref,''),COALESCE(fence_digest,''),lease_expires_at
FROM control_plane.integration_personal_credential_tests
WHERE organization_id=@organization_id::uuid AND ref=@ref FOR UPDATE
