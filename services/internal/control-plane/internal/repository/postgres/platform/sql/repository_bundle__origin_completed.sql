-- name: repository_bundle__origin_completed :many
SELECT publication.integration_invocation_id::text,publication.project_id::text
FROM control_plane.repository_bundle_publications publication
JOIN control_plane.runs root ON root.id=publication.root_run_id AND root.organization_id=publication.organization_id
JOIN control_plane.run_nodes node ON node.id=publication.origin_node_id AND node.organization_id=publication.organization_id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=publication.organization_id
WHERE publication.organization_id=@organization_id::uuid
  AND publication.origin_node_id=@node_id::uuid AND publication.origin_run_id=@run_id::uuid
  AND publication.root_run_id=@root_run_id::uuid AND publication.origin_runtime_revision_id=@runtime_revision_id::uuid
  AND publication.state IN ('SUCCEEDED','REJECTED','CANCELLED','EXPIRED','FAILED')
  AND agent.enabled AND agent.state='READY'
  AND EXISTS (SELECT 1 FROM control_plane.projects project WHERE project.id=publication.project_id AND project.lifecycle='ACTIVE')
  AND EXISTS (SELECT 1 FROM control_plane.subjects actor
    JOIN control_plane.catalog_access_targets target ON target.organization_id=actor.organization_id AND target.kind='AGENT' AND target.id=agent.id
    WHERE actor.id=root.initiated_by AND actor.organization_id=publication.organization_id AND actor.active
      AND control_plane.catalog_resource_visible(publication.organization_id,actor.id,'agent.launch','AGENT',target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false))
ORDER BY publication.id;
