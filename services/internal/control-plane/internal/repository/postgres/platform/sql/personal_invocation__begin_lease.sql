-- name: personal_invocation__begin_lease :one
SELECT invocation.claimed_recovery_read_only
FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_personal_invocation_credentials pin
 ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.integration_personal_credential_revisions revision
 ON revision.id=pin.credential_revision_id AND revision.binding_id=pin.binding_id
 AND revision.organization_id=pin.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.state='RUNNING' AND invocation.lease_ref=@lease_ref
 AND invocation.claimed_lease_ref=@lease_ref AND invocation.generation=@generation::bigint
 AND invocation.effect_fence_digest=@fence_digest AND invocation.claimed_fence_digest=@fence_digest
 AND invocation.workload_instance=@workload_instance AND invocation.claimed_workload='integration-gateway'
 AND invocation.lease_expires_at>clock_timestamp()
 AND pin.connection_id=@connection_id::uuid AND pin.root_run_id=@root_run_id::uuid
 AND pin.subject_id=@subject_id::uuid AND pin.binding_id=@binding_id::uuid
 AND pin.binding_generation=@binding_generation::bigint AND revision.ref=@credential_ref
 AND pin.definition_digest=@definition_digest AND pin.configuration_digest=@configuration_digest
 AND pin.resource_permission_profile=@resource_permission_profile;
