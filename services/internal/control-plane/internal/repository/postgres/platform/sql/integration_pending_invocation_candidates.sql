-- name: integration_pending_invocation_candidates :many
SELECT i.ref,i.state,COALESCE(g.ref,''),i.idempotency_key,i.intent_digest
FROM control_plane.integration_invocations i
LEFT JOIN control_plane.owner_gates g ON g.integration_invocation_id=i.id AND g.organization_id=i.organization_id
WHERE i.organization_id=@organization_id::uuid AND i.node_id=@node_id::uuid
 AND i.connection_id=@connection_id::uuid AND i.grant_id=@grant_id::uuid
 AND i.capability_key=@capability AND i.input_digest=@input_digest
 AND i.definition_digest=@definition_digest AND i.resource_scope_digest=@resource_scope_digest
 AND i.grant_version=@grant_version::bigint AND i.approval_policy=@approval_policy
 AND ((@alias_ref<>'' AND i.ref=@alias_ref)
   OR (@alias_ref='' AND i.state='WAITING_APPROVAL' AND g.state='OPEN'))
ORDER BY i.created_at,i.ref
LIMIT 2;
