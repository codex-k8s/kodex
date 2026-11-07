-- name: personal_invocation__context :one
SELECT invocation.id::text,connection.id::text,connection.ref,connection.credential_policy,
       connection.definition_key,connection.definition_version,connection.definition_digest,
       connection.public_configuration,invocation.capability_key,root.id::text,
       subject.id::text,subject.kind,root.source,
       control_plane.integration_connection_configuration_ready(connection.id),
       subject.active AND control_plane.catalog_resource_visible(connection.organization_id,
         subject.id,'integration.manage','INTEGRATION',connection.id,NULL,connection.created_by,
         '{}'::jsonb,statement_timestamp())
FROM control_plane.integration_invocations invocation
JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
 AND connection.organization_id=invocation.organization_id
JOIN control_plane.runs execution ON execution.id=invocation.run_id
 AND execution.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=execution.root_run_id
 AND root.organization_id=invocation.organization_id AND root.root_run_id=root.id
JOIN control_plane.subjects subject ON subject.id=root.initiated_by
 AND subject.organization_id=root.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND root.state IN ('RUNNING','WAITING_HUMAN')
ORDER BY connection.ref
FOR UPDATE OF connection;
