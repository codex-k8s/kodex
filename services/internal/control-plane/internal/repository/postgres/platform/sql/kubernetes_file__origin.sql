-- name: kubernetes_file__origin :one
SELECT origin.actor_id::text,origin.project_id::text,origin.project_ref,origin.artifact_ref,
 origin.artifact_revision_ref,origin.artifact_digest,origin.artifact_size_bytes,
 origin.object_key,origin.object_version,origin.object_etag,origin.purpose,
 agent.ref,coalesce(source_run.ref,''),entry.entry_digest,
 (agent.system_key='system-assistant' AND agent.project_id IS NULL AND agent.state<>'ARCHIVED'),own_run.ref
FROM control_plane.kubernetes_file_input_origins origin
JOIN control_plane.integration_invocations invocation ON invocation.id=origin.invocation_id AND invocation.organization_id=origin.organization_id
JOIN control_plane.runs own_run ON own_run.id=invocation.run_id AND own_run.organization_id=invocation.organization_id
JOIN control_plane.runtime_revisions revision ON revision.id=origin.runtime_revision_id AND revision.organization_id=origin.organization_id
 AND revision.ref=origin.runtime_revision_ref AND revision.revision_digest=origin.runtime_revision_digest
JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=origin.catalog_id AND catalog.organization_id=origin.organization_id
 AND catalog.ref=origin.catalog_ref AND catalog.digest=origin.catalog_digest AND catalog.frozen
 AND catalog.runtime_revision_ref=revision.ref AND catalog.actor_id=origin.actor_id
JOIN control_plane.runtime_file_catalog_entries entry ON entry.id=origin.entry_id AND entry.catalog_id=catalog.id
 AND entry.ref=origin.entry_ref AND encode(entry.entry_digest,'hex')=origin.entry_digest
 AND entry.purpose=origin.purpose AND entry.purpose=ANY(catalog.purposes)
 AND entry.artifact_id=origin.artifact_id AND entry.artifact_revision=origin.artifact_revision
 AND entry.artifact_version=origin.artifact_version AND entry.artifact_digest=origin.artifact_digest
 AND entry.size_bytes=origin.artifact_size_bytes
JOIN control_plane.artifact_history artifact ON artifact.id=origin.artifact_id AND artifact.revision_id=origin.artifact_revision_id
 AND artifact.revision_ref=origin.artifact_revision_ref AND artifact.revision=origin.artifact_revision
 AND artifact.scan_state IN('CLEAN','NOT_SCANNED')
JOIN control_plane.artifact_heads head ON head.id=origin.artifact_id AND head.organization_id=origin.organization_id AND head.lifecycle_state='ACTIVE'
JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
 AND content.object_key=origin.object_key AND content.object_version=origin.object_version AND content.object_etag=origin.object_etag
 AND content.digest=origin.artifact_digest AND content.size_bytes=origin.artifact_size_bytes
JOIN control_plane.agents agent ON agent.id=catalog.agent_id AND agent.organization_id=catalog.organization_id
LEFT JOIN control_plane.runs source_run ON source_run.id=artifact.run_id AND source_run.organization_id=origin.organization_id
WHERE origin.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND ('platform.artifact.manage'=ANY(agent.capabilities) OR(origin.purpose='RUN_RESULT' AND(
  EXISTS(SELECT 1 FROM control_plane.workflow_exception_context_node(catalog.organization_id,catalog.node_id))
  OR control_plane.runtime_file_coordinator(catalog.organization_id,catalog.actor_id,catalog.project_id,catalog.agent_id,catalog.node_id))))
 AND control_plane.runtime_file_source_visible_revision(catalog.organization_id,catalog.actor_id,catalog.project_id,catalog.agent_id,entry.artifact_id,origin.purpose,entry.source_revision_ref,catalog.node_id,entry.id);
