-- name: ssh_health__read :one
SELECT p.id::text,p.snapshot_sha256,p.input_digest,p.lease_ref,p.fence_digest,p.workload_instance,p.generation,p.expires_at,
 COALESCE(p.outcome,''),COALESCE(p.observed_subject_digest,''),p.qualified_until,COALESCE(p.terminal_state,''),COALESCE(p.applied,false)
FROM control_plane.ssh_identity_health_probes p JOIN control_plane.integration_connections c ON c.id=p.connection_id AND c.organization_id=p.organization_id
WHERE p.organization_id=@organization_id::uuid AND p.ref=@attempt_ref AND c.ref=@connection_ref
 AND (p.personal_test_id=NULLIF(@personal_id,'')::uuid OR p.shared_test_id=NULLIF(@shared_id,'')::uuid) FOR UPDATE OF p;
