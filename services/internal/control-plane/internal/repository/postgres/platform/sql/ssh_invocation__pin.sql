-- name: ssh_invocation__pin :exec
INSERT INTO control_plane.ssh_invocation_identity_bindings
 (invocation_id,organization_id,connection_id,root_run_id,root_actor_id,subject_id,binding_id,binding_generation,
 identity_revision_id,health_probe_id,subject_digest,valid_until,definition_digest,configuration_digest,transport_mode)
VALUES(@invocation_id::uuid,@organization_id::uuid,@connection_id::uuid,@root_id::uuid,@root_actor_id::uuid,@subject_id::uuid,
 NULLIF(@binding_id,'')::uuid,@generation,@identity_id::uuid,@health_id::uuid,@subject_digest,@valid_until,@definition_digest,@configuration_digest,@mode);
