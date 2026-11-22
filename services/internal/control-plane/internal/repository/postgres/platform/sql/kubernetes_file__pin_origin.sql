-- name: kubernetes_file__pin_origin :one
INSERT INTO control_plane.kubernetes_file_input_origins(
 invocation_id,organization_id,runtime_revision_id,runtime_revision_ref,runtime_revision_digest,
 catalog_id,catalog_ref,catalog_digest,entry_id,entry_ref,entry_digest,purpose,
 artifact_id,artifact_ref,artifact_revision_id,artifact_revision_ref,artifact_revision,
 artifact_version,artifact_digest,artifact_size_bytes,actor_id,project_id,project_ref,
 object_key,object_version,object_etag)
SELECT invocation.id,invocation.organization_id,revision.id,revision.ref,revision.revision_digest,
 catalog.id,catalog.ref,catalog.digest,entry.id,entry.ref,encode(entry.entry_digest,'hex'),entry.purpose,
 artifact.id,artifact.ref,artifact.revision_id,artifact.revision_ref,artifact.revision,
 entry.artifact_version,entry.artifact_digest,entry.size_bytes,catalog.actor_id,catalog.project_id,entry.project_ref,
 content.object_key,content.object_version,content.object_etag
FROM control_plane.integration_invocations invocation
JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=invocation.organization_id
JOIN control_plane.runs run ON run.id=invocation.run_id AND run.organization_id=invocation.organization_id
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN LATERAL(SELECT frozen.* FROM control_plane.runtime_revisions frozen
 WHERE frozen.node_id=node.id AND frozen.organization_id=invocation.organization_id AND frozen.created_at<=invocation.created_at
 ORDER BY frozen.generation DESC LIMIT 1) revision ON revision.run_id=run.id AND revision.root_run_id=root.id
 AND revision.agent_id=node.agent_id AND revision.turn_id IS NOT DISTINCT FROM node.turn_id
 AND revision.attempt=node.attempt AND revision.session_id=run.session_id
JOIN control_plane.runtime_file_catalogs catalog ON catalog.runtime_revision_ref=revision.ref
 AND catalog.organization_id=invocation.organization_id AND catalog.frozen
 AND catalog.node_id=node.id AND catalog.run_id=run.id AND catalog.actor_id=root.initiated_by
 AND catalog.agent_id=node.agent_id AND catalog.generation=revision.generation
JOIN control_plane.agents agent ON agent.id=catalog.agent_id AND agent.organization_id=catalog.organization_id
JOIN control_plane.runtime_file_visible_entries entry ON entry.catalog_id=catalog.id
 AND entry.purpose=ANY(catalog.purposes) AND entry.purpose IN('PROJECT','WORKSPACE_INPUT','RUN_RESULT')
 AND entry.artifact_ref=@artifact_ref AND entry.artifact_digest=@artifact_digest
JOIN control_plane.artifact_history artifact ON artifact.id=entry.artifact_id AND artifact.ref=entry.artifact_ref
 AND artifact.revision=entry.artifact_revision AND artifact.digest=entry.artifact_digest
 AND artifact.size_bytes=entry.size_bytes AND artifact.media_type=entry.media_type AND artifact.source=entry.source
JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
 AND content.digest=entry.artifact_digest AND content.size_bytes=entry.size_bytes
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND invocation.resource_kind='KUBERNETES_TARGET' AND invocation.operation='kubernetes.cp.upload'
 AND ('platform.artifact.manage'=ANY(agent.capabilities) OR(entry.purpose='RUN_RESULT' AND(
  EXISTS(SELECT 1 FROM control_plane.workflow_exception_context_node(catalog.organization_id,catalog.node_id))
  OR control_plane.runtime_file_coordinator(catalog.organization_id,catalog.actor_id,catalog.project_id,catalog.agent_id,catalog.node_id))))
 AND octet_length(content.object_version)>0 AND octet_length(content.object_etag)>0
ORDER BY CASE entry.purpose WHEN 'WORKSPACE_INPUT' THEN 0 WHEN 'PROJECT' THEN 1 ELSE 2 END,entry.ref
LIMIT 1
RETURNING invocation_id::text;
