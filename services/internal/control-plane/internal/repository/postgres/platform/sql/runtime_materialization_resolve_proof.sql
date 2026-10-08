-- name: runtime_materialization_resolve_proof :one
SELECT actor.id::text, actor.kind, actor.updated_at,
       organization.id::text, organization.version,
       CASE WHEN @system_assistant::boolean THEN '' ELSE COALESCE(project.id::text, '') END,
       CASE WHEN @system_assistant::boolean THEN 0 ELSE COALESCE(project.version, 0) END,
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
LEFT JOIN control_plane.assistant_conversations conversation
  ON conversation.session_id = session.id AND conversation.organization_id = revision.organization_id
  AND conversation.assistant_agent_id = agent.id
LEFT JOIN control_plane.project_assistant_profiles assistant_profile
  ON assistant_profile.id = conversation.assistant_profile_id
  AND assistant_profile.agent_id = agent.id AND assistant_profile.organization_id = revision.organization_id
  AND assistant_profile.project_id = agent.project_id
WHERE lease.materialization_operation = @operation
  AND lease.materialization_request_digest = @request_digest
  AND lease.state = 'CLAIMED' AND lease.expires_at > clock_timestamp() AND control_plane.runtime_execution_before_deadline(lease.organization_id,lease.run_id)
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
        AND agent.project_id = project.id AND revision.safe_snapshot ->> 'assistantScope' IN ('NONE', 'PROJECT')
        AND (revision.safe_snapshot ->> 'assistantScope' = 'NONE'
          OR (conversation.assistant_scope = 'PROJECT' AND conversation.state = 'ACTIVE'
            AND conversation.created_by = actor.id AND conversation.project_id = project.id
            AND assistant_profile.ref = revision.safe_snapshot ->> 'assistantProfileRef'
            AND session.target_type = 'SYSTEM_ASSISTANT' AND session.target_ref = agent.ref
            AND root_run.target_type = 'SYSTEM_ASSISTANT' AND root_run.target_ref = agent.ref
            AND root_run.session_id = session.id AND revision.run_id = root_run.id))
        AND root_run.project_id = project.id AND execution_run.project_id = project.id
        AND session.project_id = project.id)
    OR (@system_assistant::boolean AND @project_ref = ''
        AND revision.safe_snapshot ->> 'assistantScope' = 'SYSTEM'
        AND root_run.project_id IS NOT DISTINCT FROM revision.project_id
        AND execution_run.project_id IS NOT DISTINCT FROM revision.project_id
        AND session.project_id IS NOT DISTINCT FROM revision.project_id
        AND agent.system_key = 'system-assistant' AND agent.project_id IS NULL
        AND session.target_type = 'SYSTEM_ASSISTANT' AND session.target_ref = agent.ref
        AND root_run.target_type = 'SYSTEM_ASSISTANT' AND root_run.target_ref = agent.ref
        AND conversation.assistant_scope = 'SYSTEM' AND conversation.assistant_profile_id IS NULL
        AND conversation.state = 'ACTIVE' AND conversation.created_by = actor.id
        AND conversation.project_id IS NOT DISTINCT FROM revision.project_id
        AND session.created_by = actor.id AND revision.run_id = root_run.id
        AND root_run.session_id = session.id AND revision.turn_id IS NOT NULL));
