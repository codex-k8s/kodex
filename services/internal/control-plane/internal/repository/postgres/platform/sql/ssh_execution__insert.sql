-- name: ssh_execution__insert :exec
INSERT INTO control_plane.ssh_execution_intents(ref,organization_id,invocation_id,identity_revision_id,workload_instance,
 lease_ref,fence_digest,generation,input_digest,snapshot_body,snapshot_digest,created_at,expires_at)
VALUES(@ref,@organization_id::uuid,@invocation_id::uuid,@identity_id::uuid,@instance,@lease_ref,@fence_digest,@generation,
 @input_digest,@snapshot,@snapshot_digest,@created_at,@expires_at)
ON CONFLICT(invocation_id) DO NOTHING;
