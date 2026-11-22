-- name: kubernetes_execution__cleanup :one
WITH observed AS MATERIALIZED(SELECT clock_timestamp() AS now)
SELECT invocation.ref,stream.ref,stream.generation,plan.binding_digest,plan.snapshot_digest,stream.state,
 stream.state NOT IN('PREPARED','ACTIVE') OR stream.hard_deadline<=observed.now OR plan.hard_deadline<=observed.now
 OR invocation.state<>'RUNNING' OR invocation.generation<>@generation
 OR invocation.lease_ref IS DISTINCT FROM @lease_ref
 OR invocation.effect_fence_digest IS DISTINCT FROM @fence_digest
 OR invocation.workload_instance IS DISTINCT FROM @instance
 OR invocation.lease_expires_at IS NULL OR invocation.lease_expires_at<=observed.now
FROM control_plane.integration_invocations invocation
JOIN control_plane.kubernetes_execution_plans plan ON plan.invocation_id=invocation.id AND plan.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.plan_id=plan.id AND child.organization_id=plan.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.child_id=child.id AND effect.organization_id=child.organization_id
JOIN control_plane.kubernetes_execution_streams stream ON stream.effect_id=effect.id AND stream.organization_id=effect.organization_id
CROSS JOIN observed
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.resource_kind='KUBERNETES_TARGET' AND invocation.input_digest=@input_digest
 AND plan.binding_digest=@binding_digest AND stream.ref=@stream_ref AND stream.generation=@generation;
