-- name: queries_getoverview_select_projects_organization_id_lifecycle_state :one
SELECT
    (SELECT count(*) FROM control_plane.projects project
     WHERE project.organization_id=@organization_id::uuid AND project.lifecycle='ACTIVE'
       AND (@project_ref='' OR project.ref=@project_ref)
       AND (@authority_project_id='' OR project.id=NULLIF(@authority_project_id,'')::uuid)
       AND EXISTS(SELECT 1 FROM control_plane.assistant_context_projection_v2(
           project.organization_id,@actor_id::uuid,NULLIF(@authority_project_id,'')::uuid,
           'PROJECT',project.ref,statement_timestamp()))),
    (SELECT count(*) FROM control_plane.agents agent
     JOIN control_plane.projects project ON project.id=agent.project_id
     JOIN control_plane.subjects creator ON creator.id=agent.created_by
     JOIN control_plane.role_definitions role ON role.id=agent.role_definition_id
     JOIN control_plane.runtime_profiles runtime ON runtime.stable_key=agent.runtime_key
     JOIN control_plane.agent_runtime_config_versions configuration ON configuration.id=agent.current_runtime_config_id
     WHERE agent.organization_id=@organization_id::uuid AND agent.system_key IS NULL AND agent.state<>'ARCHIVED'
       AND (@project_ref='' OR project.ref=@project_ref)
       AND (@authority_project_id='' OR agent.project_id=NULLIF(@authority_project_id,'')::uuid)
       AND control_plane.catalog_resource_visible(agent.organization_id,@actor_id::uuid,'agent.view','AGENT',
           agent.id,agent.project_id,agent.created_by,jsonb_build_object('PROJECT',agent.project_id::text),statement_timestamp()));
