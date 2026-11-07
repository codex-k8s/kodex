-- name: personal_credential_test__reset_binding :exec
UPDATE control_plane.integration_personal_credentials
SET state='CHECK_REQUIRED',version=version+1
WHERE organization_id=@organization_id::uuid AND id=@binding_id::uuid AND state='CHECKING'
 AND version=@version AND generation=@generation
