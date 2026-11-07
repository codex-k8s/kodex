-- name: personal_invocation__pin :exec
INSERT INTO control_plane.integration_personal_invocation_credentials
 (invocation_id,organization_id,connection_id,root_run_id,subject_id,binding_id,
  binding_generation,credential_revision_id,definition_digest,configuration_digest,resource_permission_profile)
SELECT @invocation_id::uuid,@organization_id::uuid,@connection_id::uuid,@root_run_id::uuid,
 @subject_id::uuid,binding.id,@binding_generation::bigint,revision.id,
 @definition_digest,@configuration_digest,@resource_permission_profile
FROM control_plane.integration_personal_credentials binding
JOIN control_plane.integration_personal_credential_revisions revision
 ON revision.binding_id=binding.id AND revision.organization_id=binding.organization_id
 AND revision.ref=@credential_ref AND revision.id=binding.credential_revision_id
WHERE binding.organization_id=@organization_id::uuid AND binding.ref=@binding_ref
 AND binding.id=@binding_id::uuid AND binding.connection_id=@connection_id::uuid
 AND binding.subject_id=@subject_id::uuid AND binding.generation=@binding_generation::bigint
 AND binding.state='READY'
ON CONFLICT (invocation_id) DO NOTHING;
