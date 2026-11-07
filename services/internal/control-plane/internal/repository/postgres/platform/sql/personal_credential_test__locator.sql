-- name: personal_credential_test__locator :one
SELECT connection.ref,test.subject_id::text,test.binding_id::text
FROM control_plane.integration_personal_credential_tests test
JOIN control_plane.integration_connections connection ON connection.id=test.connection_id AND connection.organization_id=test.organization_id
WHERE test.organization_id=@organization_id::uuid AND test.ref=@ref
