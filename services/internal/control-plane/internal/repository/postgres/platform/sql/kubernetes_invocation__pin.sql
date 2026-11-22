-- name: kubernetes_invocation__pin :exec
INSERT INTO control_plane.kubernetes_invocation_identity_bindings(
 invocation_id,organization_id,connection_id,root_run_id,root_actor_id,subject_id,binding_id,binding_generation,
 identity_revision_id,health_attempt_id,credential_policy,definition_digest,configuration_digest,
 registry_revision,target_ref,target_revision,subject_digest)
VALUES(@invocation_id::uuid,@organization_id::uuid,@connection_id::uuid,@root_id::uuid,@root_actor_id::uuid,
 @subject_id::uuid,NULLIF(@binding_id,'')::uuid,@binding_generation,@identity_id::uuid,@health_id::uuid,@policy,
 @definition_digest,@configuration_digest,@registry_revision,@target_ref,@target_revision,@subject_digest);
