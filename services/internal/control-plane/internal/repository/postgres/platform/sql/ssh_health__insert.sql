-- name: ssh_health__insert :exec
WITH stamp AS MATERIALIZED (SELECT clock_timestamp() AS at)
INSERT INTO control_plane.ssh_identity_health_probes(ref,organization_id,connection_id,identity_revision_id,personal_test_id,shared_test_id,snapshot_sha256,input_digest,lease_ref,fence_digest,workload_instance,generation,expires_at,definition_digest,configuration_digest,transport_mode,created_at)
SELECT @ref,@organization_id::uuid,c.id,@identity_id::uuid,NULLIF(@personal_id,'')::uuid,NULLIF(@shared_id,'')::uuid,@snapshot_digest,@input_digest,@lease_ref,@fence_digest,@instance,@generation,@expires_at,@definition_digest,@configuration_digest,@mode,stamp.at
FROM control_plane.integration_connections c CROSS JOIN stamp
WHERE c.organization_id=@organization_id::uuid AND c.ref=@connection_ref AND @expires_at::timestamptz>stamp.at;
