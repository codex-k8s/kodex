-- name: kubernetes_input__public_origin :one
SELECT invocation.id::text,connection.ref,run.ref,node.ref
FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=invocation.id AND pin.organization_id=invocation.organization_id
WHERE invocation.ref=@invocation_ref AND invocation.organization_id=@organization_id::uuid
 AND invocation.resource_kind='KUBERNETES_TARGET' AND pin.root_actor_id=@actor_id::uuid AND root.initiated_by=pin.root_actor_id
 AND pin.root_run_id=root.id AND pin.connection_id=connection.id
 AND (@project_ref='' OR EXISTS(SELECT 1 FROM control_plane.projects project WHERE project.id=run.project_id AND project.organization_id=run.organization_id AND project.ref=@project_ref));
