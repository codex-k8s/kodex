-- name: personal_invocation__admit :exec
INSERT INTO control_plane.integration_personal_invocation_admissions
 (organization_id,invocation_id,generation,lease_ref,fence_digest,workload_instance,recovery_read_only)
VALUES (@organization_id::uuid,@invocation_id::uuid,@generation::bigint,@lease_ref,@fence_digest,@workload_instance,@recovery_read_only)
ON CONFLICT DO NOTHING;
