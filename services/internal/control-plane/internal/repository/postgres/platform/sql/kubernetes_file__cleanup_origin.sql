-- name: kubernetes_file__cleanup_origin :one
SELECT invocation.id::text,root.id::text,root.ref,coalesce(project.ref,''),origin.project_id::text,origin.project_ref
FROM control_plane.integration_invocations invocation
JOIN control_plane.kubernetes_file_input_origins origin ON origin.invocation_id=invocation.id AND origin.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id AND root.id=run.root_run_id
LEFT JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
WHERE invocation.ref=@invocation_ref AND invocation.organization_id=@organization_id::uuid
 AND invocation.operation='kubernetes.cp.upload' AND invocation.resource_kind='KUBERNETES_TARGET'
 AND origin.actor_id=@actor_id::uuid AND pin.root_actor_id=origin.actor_id AND root.initiated_by=origin.actor_id
 AND (@project_ref='' OR project.ref=@project_ref);
