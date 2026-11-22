-- name: ssh_execution__lease :one
WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at)
SELECT invocation.id::text,invocation.ref,invocation.operation,invocation.bounded_input,
 invocation.input_digest,invocation.effect_key,invocation.node_id::text,run.root_run_id::text,
 COALESCE(run.project_id::text,''),initiator.ref,
 least(invocation.lease_expires_at-interval '5 seconds',pin.valid_until-interval '5 seconds',approval.expires_at-interval '5 seconds',stamp.at+interval '25 seconds'),stamp.at
FROM control_plane.integration_invocations invocation
JOIN control_plane.ssh_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
LEFT JOIN control_plane.integration_approval_scopes approval ON approval.id=invocation.approval_scope_id AND approval.organization_id=invocation.organization_id
JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN control_plane.subjects initiator ON initiator.id=root.initiated_by AND initiator.organization_id=root.organization_id
CROSS JOIN stamp
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.state='RUNNING' AND invocation.claimed_workload='integration-gateway'
 AND invocation.workload_instance=@instance AND invocation.lease_ref=@lease_ref
 AND invocation.generation=@generation AND invocation.effect_fence_digest=@fence_digest AND invocation.input_digest=@input_digest
 AND invocation.lease_expires_at>stamp.at AND control_plane.ssh_invocation_identity_current(pin)
 AND (invocation.approval_policy<>'HUMAN_SCOPED' OR (approval.id IS NOT NULL AND approval.revoked_at IS NULL AND approval.expires_at>stamp.at))
FOR UPDATE OF invocation;
