-- name: personal_credential_test__insert :exec
INSERT INTO control_plane.integration_personal_credential_tests
 (ref,organization_id,connection_id,binding_id,subject_id,credential_revision_id,input_snapshot)
SELECT @ref,binding.organization_id,binding.connection_id,binding.id,binding.subject_id,binding.credential_revision_id,@snapshot::jsonb
FROM control_plane.integration_personal_credentials binding
WHERE binding.organization_id=@organization_id::uuid AND binding.id=@binding_id::uuid
 AND binding.subject_id=@subject_id::uuid AND binding.version=@version AND binding.state='CHECKING'
