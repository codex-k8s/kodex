-- name: runtime_recordtoolcall_select_actor_and_grant :one
SELECT agent.ref,agent.name,COALESCE(agent.system_key='system-assistant',false),
       COALESCE(agent.system_key='system-assistant',false) OR EXISTS (
         SELECT 1
         FROM control_plane.run_nodes node
         JOIN control_plane.runs run
           ON run.id=node.run_id AND run.organization_id=revision.organization_id
         JOIN control_plane.runs root
           ON root.id=run.root_run_id AND root.organization_id=revision.organization_id
         JOIN control_plane.assistant_conversations conversation
           ON conversation.organization_id=revision.organization_id AND conversation.session_id=revision.session_id
          AND conversation.assistant_agent_id=agent.id AND conversation.project_id=revision.project_id
          AND conversation.created_by=root.initiated_by AND conversation.assistant_scope='PROJECT'
          AND conversation.state='ACTIVE'
         JOIN control_plane.sessions session
           ON session.id=conversation.session_id AND session.organization_id=revision.organization_id
          AND session.project_id=revision.project_id AND session.created_by=conversation.created_by
          AND session.target_type='SYSTEM_ASSISTANT' AND session.target_ref=agent.ref
         JOIN control_plane.project_assistant_profiles profile
           ON profile.id=conversation.assistant_profile_id AND profile.organization_id=revision.organization_id
          AND profile.project_id=revision.project_id AND profile.agent_id=agent.id
         JOIN control_plane.projects project
           ON project.id=profile.project_id AND project.organization_id=revision.organization_id AND project.lifecycle='ACTIVE'
         JOIN control_plane.subjects actor
           ON actor.id=root.initiated_by AND actor.organization_id=revision.organization_id AND actor.active AND actor.kind='USER'
         WHERE node.id=revision.node_id AND node.organization_id=revision.organization_id
           AND node.agent_id=agent.id AND node.state='RUNNING' AND node.turn_id IS NOT DISTINCT FROM revision.turn_id
           AND run.id=revision.run_id AND run.root_run_id=revision.root_run_id
           AND run.session_id=revision.session_id AND run.project_id=revision.project_id
           AND run.target_type='SYSTEM_ASSISTANT' AND run.target_ref=agent.ref
           AND run.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELED')
           AND root.state NOT IN ('SUCCEEDED','FAILED','CANCELLED','CANCELED')
           AND agent.system_key IS NULL AND agent.project_id=revision.project_id
           AND agent.enabled AND agent.state IN ('READY','RUNNING')
           AND revision.safe_snapshot->>'assistantScope'='PROJECT'
           AND revision.safe_snapshot->>'assistantProfileRef'=profile.ref
           AND revision.safe_snapshot->>'agentRef'=agent.ref
           AND revision.safe_snapshot->>'projectRef'=project.ref
       ),
       CASE
         WHEN @tool='launch_workflow' AND @grant_ref='' THEN
           'platform.run.launch'=ANY(revision.capabilities) AND 'platform.run.launch'=ANY(agent.capabilities)
           AND agent.enabled AND agent.state IN ('READY','RUNNING') AND agent.system_key IS NULL
           AND revision.safe_snapshot->>'assistantScope'='NONE'
         WHEN @grant_ref='' THEN true
         WHEN @capability_ref='' AND @tool IN ('search_files','get_file_metadata','preview_file','get_file_manifest','read_file') THEN EXISTS (
           SELECT 1 FROM control_plane.runtime_file_catalogs catalog
           WHERE catalog.runtime_revision_ref=revision.ref AND catalog.organization_id=revision.organization_id
             AND catalog.ref=@grant_ref AND catalog.generation=revision.generation AND catalog.frozen
             AND @purpose=ANY(catalog.purposes)
         )
         ELSE EXISTS (
           SELECT 1
           FROM jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) integration_grant(value)
           WHERE integration_grant.value->>'ref'=@grant_ref AND integration_grant.value->>'capabilityKey'=@capability_ref
         )
       END
FROM control_plane.runtime_revisions revision
JOIN control_plane.agents agent ON agent.id=revision.agent_id AND agent.organization_id=revision.organization_id
WHERE revision.organization_id=@organization_id::uuid AND revision.node_id=@node_id::uuid
  AND revision.generation=@generation
