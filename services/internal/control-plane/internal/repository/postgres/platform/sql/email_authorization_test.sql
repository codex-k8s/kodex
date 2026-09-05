-- name: email_authorization_test :one
SELECT t.id::text,c.ref,actor.ref,'','','',t.ref,'email.delivery.health.read','',
       '{}'::jsonb,'{}'::jsonb,c.public_configuration,c.definition_version,c.definition_digest,'READ',false
FROM control_plane.integration_connection_tests t
JOIN control_plane.integration_connections c ON c.id=t.connection_id AND c.organization_id=t.organization_id
JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
JOIN control_plane.subjects actor ON actor.id=t.created_by AND actor.organization_id=t.organization_id
WHERE t.organization_id=$1::uuid AND t.ref=$2 AND t.state='CLAIMED'
  AND t.claimed_workload='integration-gateway' AND t.claimed_lease_ref=$3 AND t.claimed_fence_digest=$4
  AND t.lease_ref=$3 AND t.fence_digest=$4 AND t.generation=$5
  AND t.lease_expires_at=$6 AND t.lease_expires_at>clock_timestamp()
  AND c.enabled AND c.state='TESTING' AND c.definition_key='email'
  AND d.enabled AND d.adapter_owner='integration-gateway' AND d.execution_route='MANAGED_MCP' AND d.adapter_readiness='READY'
FOR SHARE OF t,c,d,actor;
