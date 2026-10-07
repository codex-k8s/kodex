-- name: project_assistant_connection__owner :one
SELECT project.ref, profile.ref, profile.version, agent.version, agent.name
FROM control_plane.project_assistant_profiles profile
JOIN control_plane.projects project ON project.id=profile.project_id AND project.organization_id=profile.organization_id
JOIN control_plane.agents agent ON agent.id=profile.agent_id AND agent.organization_id=profile.organization_id
  AND agent.project_id=profile.project_id AND agent.system_key IS NULL
WHERE profile.organization_id=@organization_id::uuid AND agent.ref=@assistant_ref
  AND project.lifecycle='ACTIVE' AND agent.enabled AND agent.state<>'ARCHIVED'
  AND (@authority_project='' OR project.id=NULLIF(@authority_project,'')::uuid)
  AND control_plane.catalog_resource_visible(profile.organization_id,@actor_id::uuid,'project.manage','PROJECT',
      project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())
  AND EXISTS (SELECT 1 FROM control_plane.memberships member JOIN control_plane.subjects actor
      ON actor.id=member.subject_id AND actor.organization_id=member.organization_id AND actor.active
      WHERE member.organization_id=profile.organization_id AND member.subject_id=@actor_id::uuid
        AND member.project_id IS NULL AND member.active AND member.role IN ('OWNER','ADMINISTRATOR'))
FOR SHARE OF profile,project,agent;
