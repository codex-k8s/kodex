-- name: kubernetes_capture__public_origin :one
SELECT capture.id::text,capture.stream_id::text,root.id::text,root.ref,coalesce(project.ref,'')
FROM control_plane.kubernetes_output_captures capture
JOIN control_plane.kubernetes_execution_streams stream ON stream.id=capture.stream_id AND stream.organization_id=capture.organization_id
JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id AND root.id=run.root_run_id
LEFT JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
WHERE capture.organization_id=@organization_id::uuid AND capture.ref=@capture_ref
 AND pin.root_actor_id=@actor_id::uuid AND root.initiated_by=pin.root_actor_id
 AND (@project_ref='' OR project.ref=@project_ref)
 AND capture.source_snapshot::jsonb->>'organizationId'=capture.organization_id::text
 AND capture.source_snapshot::jsonb->>'rootActorId'=pin.root_actor_id::text
 AND capture.source_snapshot::jsonb->>'rootRunId'=root.id::text
 AND capture.source_snapshot::jsonb->>'runId'=run.id::text
 AND capture.source_snapshot::jsonb->>'invocationRef'=invocation.ref
 AND capture.source_snapshot::jsonb->>'streamRef'=stream.ref
 AND capture.source_snapshot::jsonb->>'bindingDigest'=plan.binding_digest
 AND capture.source_snapshot::jsonb->>'snapshotDigest'=plan.snapshot_digest;
