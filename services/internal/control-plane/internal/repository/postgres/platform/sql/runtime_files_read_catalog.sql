-- name: runtime_files_read_catalog :one
SELECT catalog.id::text,catalog.ref,catalog.digest,catalog.total,catalog.purposes,
    catalog.actor_id::text,catalog.project_id::text
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
JOIN control_plane.runtime_file_catalogs catalog ON catalog.runtime_revision_ref=revision.ref AND catalog.frozen
JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.id=catalog.node_id AND node.state='RUNNING'
JOIN control_plane.runs run ON run.id=lease.run_id AND run.id=catalog.run_id
JOIN control_plane.runs root ON root.id=revision.root_run_id AND root.initiated_by=catalog.actor_id
JOIN control_plane.subjects actor ON actor.id=catalog.actor_id AND actor.organization_id=lease.organization_id AND actor.active
JOIN control_plane.catalog_access_targets project
  ON project.organization_id=lease.organization_id AND project.kind='PROJECT' AND project.id=catalog.project_id
LEFT JOIN control_plane.catalog_access_targets agent
  ON agent.organization_id=lease.organization_id AND agent.kind='AGENT' AND agent.id=catalog.agent_id
JOIN control_plane.agents current_agent ON current_agent.id=catalog.agent_id AND current_agent.organization_id=catalog.organization_id
WHERE lease.organization_id=@organization_id::uuid AND catalog.organization_id=lease.organization_id
  AND lease.ref=@lease_ref AND lease.fence_digest=@fence_digest AND lease.generation=@generation
  AND lease.generation=catalog.generation AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
  AND catalog.ref=@catalog_ref AND catalog.digest=@catalog_digest AND @purpose=ANY(catalog.purposes)
  AND (@authority_project='' OR catalog.project_id=NULLIF(@authority_project,'')::uuid)
  AND run.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELED')
  AND root.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELED')
  AND control_plane.catalog_resource_visible(catalog.organization_id,catalog.actor_id,'project.view','PROJECT',
      project.id,project.project_id,project.owner_id,project.related_ids,statement_timestamp(),false)
  AND (control_plane.catalog_resource_visible(catalog.organization_id,catalog.actor_id,'agent.view','AGENT',
      agent.id,agent.project_id,agent.owner_id,agent.related_ids,statement_timestamp(),false)
      OR (current_agent.system_key='system-assistant' AND current_agent.project_id IS NULL AND current_agent.state<>'ARCHIVED'))
FOR SHARE OF lease;
