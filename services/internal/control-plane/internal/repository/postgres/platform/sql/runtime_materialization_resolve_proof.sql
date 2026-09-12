-- name: runtime_materialization_resolve_proof :one
SELECT actor.id::text, actor.kind, actor.updated_at,
       organization.id::text, organization.version,
       COALESCE(project.id::text, ''), COALESCE(project.version, 0),
       revision.id::text, revision.generation, revision.revision_digest, lease.expires_at
FROM control_plane.runtime_leases lease
JOIN control_plane.organizations organization
  ON organization.id = lease.organization_id
JOIN control_plane.runtime_revisions revision
  ON revision.id = lease.runtime_revision_id AND revision.organization_id = lease.organization_id
JOIN control_plane.runs root_run
  ON root_run.id = revision.root_run_id AND root_run.organization_id = lease.organization_id
JOIN control_plane.runs execution_run
  ON execution_run.id = revision.run_id AND execution_run.organization_id = lease.organization_id
JOIN control_plane.run_nodes node
  ON node.id = revision.node_id AND node.organization_id = lease.organization_id
JOIN control_plane.sessions session
  ON session.id = revision.session_id AND session.organization_id = lease.organization_id
LEFT JOIN control_plane.session_turns turn
  ON turn.id = revision.turn_id AND turn.organization_id = lease.organization_id
JOIN control_plane.subjects actor
  ON actor.id = root_run.initiated_by AND actor.organization_id = lease.organization_id
JOIN control_plane.agents agent
  ON agent.id = revision.agent_id AND agent.organization_id = lease.organization_id
LEFT JOIN control_plane.projects project
  ON project.id = revision.project_id AND project.organization_id = lease.organization_id
WHERE lease.materialization_operation = @operation
  AND lease.materialization_request_digest = @request_digest
  AND lease.state = 'CLAIMED' AND lease.expires_at > clock_timestamp()
  AND lease.run_id = revision.run_id AND lease.node_id = revision.node_id
  AND lease.generation = revision.generation AND lease.input_digest = revision.input_digest
  AND node.run_id = execution_run.id AND node.root_run_id = root_run.id
  AND node.attempt = revision.attempt AND actor.active
  AND (revision.turn_id IS NULL OR
       (turn.session_id = session.id AND turn.run_id = execution_run.id))
  AND NOT EXISTS (
      SELECT 1 FROM control_plane.runtime_leases newer
      WHERE newer.organization_id = lease.organization_id AND newer.node_id = lease.node_id
        AND newer.generation > lease.generation
  )
  AND ((NOT @system_assistant::boolean
        AND project.ref = @project_ref AND project.lifecycle = 'ACTIVE'
        AND root_run.project_id = project.id AND execution_run.project_id = project.id
        AND session.project_id = project.id)
    OR (@system_assistant::boolean AND @project_ref = ''
        AND revision.project_id IS NULL AND root_run.project_id IS NULL
        AND execution_run.project_id IS NULL AND session.project_id IS NULL
        AND agent.system_key = 'system-assistant' AND agent.project_id IS NULL
        AND session.target_type = 'SYSTEM_ASSISTANT' AND session.target_ref = 'system-assistant'
        AND session.created_by = actor.id AND revision.run_id = root_run.id
        AND root_run.session_id = session.id AND revision.turn_id IS NOT NULL));
