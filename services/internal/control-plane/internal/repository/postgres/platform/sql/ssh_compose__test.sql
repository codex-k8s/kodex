-- name: ssh_compose__test :one
SELECT t.id::text,c.ref,c.public_configuration,p.id::text,p.ref,initiator.ref,
       t.lease_expires_at,t.ssh_compose_snapshot,t.attempt
FROM control_plane.integration_connection_tests t
JOIN control_plane.integration_connections c ON c.id=t.connection_id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
JOIN control_plane.projects p ON p.organization_id=c.organization_id AND p.ref=c.public_configuration->>'project_ref' AND p.lifecycle='ACTIVE'
JOIN control_plane.subjects initiator ON initiator.id=t.created_by
WHERE t.organization_id=@organization_id::uuid AND t.ref=@test_ref
  AND t.state='CLAIMED' AND t.claimed_workload='integration-gateway'
  AND t.lease_ref=@lease_ref AND t.generation=@generation AND t.fence_digest=@fence_digest
  AND t.workload_instance=@workload_instance AND t.lease_expires_at>clock_timestamp()
  AND c.definition_key='ssh-compose' AND c.enabled AND c.state='TESTING'
  AND d.enabled AND d.adapter_readiness='READY' AND d.adapter_owner='integration-gateway'
FOR UPDATE OF t NOWAIT FOR SHARE OF c NOWAIT;
