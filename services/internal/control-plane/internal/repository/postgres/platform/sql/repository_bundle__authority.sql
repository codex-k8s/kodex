-- name: repository_bundle__authority :one
SELECT initiator.ref
FROM control_plane.repository_bundle_publications publication
JOIN control_plane.runs root ON root.id=publication.root_run_id AND root.organization_id=publication.organization_id
JOIN control_plane.run_nodes origin_node ON origin_node.id=publication.origin_node_id AND origin_node.organization_id=publication.organization_id
JOIN control_plane.agents origin_agent ON origin_agent.id=origin_node.agent_id AND origin_agent.organization_id=publication.organization_id
JOIN control_plane.projects project ON project.id=publication.project_id AND project.organization_id=publication.organization_id
JOIN control_plane.subjects initiator ON initiator.id=publication.root_actor_id AND initiator.organization_id=publication.organization_id AND initiator.active
JOIN control_plane.owner_gates gate ON gate.id=publication.owner_gate_id AND gate.organization_id=publication.organization_id
JOIN control_plane.integration_connections connection ON connection.id=publication.connection_id AND connection.organization_id=publication.organization_id
JOIN control_plane.integration_grants grant_binding ON grant_binding.id=publication.grant_id AND grant_binding.organization_id=publication.organization_id AND grant_binding.connection_id=connection.id
JOIN control_plane.integration_credential_revisions credential ON credential.id=publication.credential_revision_id AND credential.connection_id=connection.id AND credential.organization_id=publication.organization_id
WHERE publication.organization_id=@organization_id::uuid AND publication.id=@id::uuid
  AND root.state IN ('RUNNING','WAITING_HUMAN')
  AND project.lifecycle='ACTIVE' AND origin_agent.project_id=project.id
  AND origin_agent.enabled AND origin_agent.state='READY' AND origin_agent.system_key IS NULL
  AND gate.state=CASE WHEN @approved::boolean THEN 'APPROVED' ELSE 'OPEN' END
  AND connection.lifecycle_state='ACTIVE' AND connection.enabled AND connection.state='CONNECTED'
  AND connection.version=@connection_version AND connection.definition_version=@definition_version
  AND connection.definition_digest=@definition_digest AND connection.credential_revision_id=credential.id
  AND grant_binding.enabled AND grant_binding.version=@grant_version
  AND grant_binding.definition_version=connection.definition_version AND grant_binding.definition_digest=connection.definition_digest
  AND grant_binding.capability_key='github.repository.bundle.publish'
  AND grant_binding.risk='WRITE' AND grant_binding.approval_policy='HUMAN_EACH_EFFECT'
  AND grant_binding.ref=@grant_ref
  AND grant_binding.target_kind='AGENT' AND grant_binding.target_ref=origin_agent.ref
  AND control_plane.runtime_execution_before_deadline(publication.organization_id,publication.origin_run_id)
FOR SHARE OF root,initiator,origin_agent,project,connection,grant_binding,credential NOWAIT;
