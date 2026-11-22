-- name: kubernetes_execution__child_insert :one
INSERT INTO control_plane.kubernetes_execution_children(ref,organization_id,plan_id,ordinal,operation,risk,approval_policy,capability_key,grant_id,grant_version,grant_scope_digest,input_json,input_digest,observation_json,node_pin_ref,node_pin_generation)
SELECT @child_ref,@organization_id::uuid,@plan_id::uuid,@ordinal,@operation,@risk,@approval_policy,@capability_key,
 grant_binding.id,@grant_version,@grant_scope_digest,@input_json,@input_digest,@observation_json::jsonb,nullif(@node_ref,''),nullif(@node_generation::bigint,0)
FROM control_plane.integration_grants grant_binding
WHERE grant_binding.organization_id=@organization_id::uuid AND grant_binding.ref=@grant_ref
 AND grant_binding.version=@grant_version AND grant_binding.enabled
RETURNING id::text;
