-- name: assistant_search_resolve_lease :one
SELECT actor.ref, actor.id::text,
       CASE WHEN conversation.assistant_scope='PROJECT' THEN conversation.project_id::text ELSE '' END,
       CASE WHEN conversation.assistant_scope='PROJECT' THEN project.ref ELSE '' END
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision
  ON revision.id=lease.runtime_revision_id AND revision.organization_id=lease.organization_id
JOIN control_plane.run_nodes node
  ON node.id=lease.node_id AND node.organization_id=lease.organization_id AND node.run_id=lease.run_id AND node.state='RUNNING'
JOIN control_plane.runs run
  ON run.id=lease.run_id AND run.organization_id=lease.organization_id
JOIN control_plane.runs root
  ON root.id=run.root_run_id AND root.organization_id=lease.organization_id
JOIN control_plane.agents agent
  ON agent.id=node.agent_id AND agent.organization_id=lease.organization_id
JOIN control_plane.assistant_conversations conversation
  ON conversation.organization_id=run.organization_id AND conversation.session_id=run.session_id
 AND conversation.assistant_agent_id=agent.id AND conversation.created_by=root.initiated_by AND conversation.state='ACTIVE'
JOIN control_plane.sessions session
  ON session.id=conversation.session_id AND session.organization_id=conversation.organization_id
 AND session.created_by=conversation.created_by AND session.target_ref=agent.ref
LEFT JOIN control_plane.project_assistant_profiles profile
  ON profile.id=conversation.assistant_profile_id AND profile.organization_id=conversation.organization_id
 AND profile.agent_id=agent.id AND profile.project_id=conversation.project_id
LEFT JOIN control_plane.projects project
  ON project.id=conversation.project_id AND project.organization_id=conversation.organization_id
JOIN control_plane.subjects actor
  ON actor.id=root.initiated_by AND actor.organization_id=lease.organization_id
WHERE lease.organization_id=@organization_id::uuid
  AND lease.ref=@lease_ref
  AND lease.fence_digest=@fence_digest
  AND lease.generation=@generation
  AND lease.state='CLAIMED'
  AND lease.expires_at>clock_timestamp()
  AND run.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELED')
  AND root.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELED')
  AND run.target_type='SYSTEM_ASSISTANT' AND run.target_ref=agent.ref
  AND revision.run_id=run.id AND revision.node_id=node.id AND revision.agent_id=agent.id
  AND revision.root_run_id=root.id AND revision.session_id=session.id
  AND revision.turn_id IS NOT DISTINCT FROM node.turn_id
  AND revision.generation=lease.generation
  AND ((conversation.assistant_scope='SYSTEM' AND agent.system_key='system-assistant' AND agent.project_id IS NULL)
    OR (conversation.assistant_scope='PROJECT' AND agent.system_key IS NULL AND profile.id IS NOT NULL AND project.lifecycle='ACTIVE'))
  AND agent.state<>'ARCHIVED'
  AND revision.safe_snapshot->>'assistantScope'=conversation.assistant_scope
  AND revision.safe_snapshot->>'agentRef'=agent.ref
  AND COALESCE(revision.safe_snapshot->>'assistantProfileRef','')=COALESCE(profile.ref,'')
  AND actor.active AND actor.kind='USER'
FOR SHARE OF lease;
