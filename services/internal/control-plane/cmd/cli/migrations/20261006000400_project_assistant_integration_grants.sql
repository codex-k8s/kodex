-- +goose Up
SET ROLE control_plane_owner;

-- Доступность операции не зависит от экрана и не назначает права самим помощником.
-- +goose StatementBegin
CREATE FUNCTION control_plane.assistant_project_integration_grant_operations(
    p_tenant uuid, p_actor uuid, p_agent uuid, p_scope text, p_authority_project uuid
) RETURNS text[] LANGUAGE sql STABLE SECURITY INVOKER
SET search_path = pg_catalog, control_plane AS $$
SELECT CASE WHEN p_scope='PROJECT' AND EXISTS (
    SELECT 1 FROM control_plane.project_assistant_profiles profile
    JOIN control_plane.projects project ON project.id=profile.project_id AND project.organization_id=profile.organization_id
    JOIN control_plane.agents agent ON agent.id=profile.agent_id AND agent.organization_id=profile.organization_id
      AND agent.project_id=profile.project_id AND agent.system_key IS NULL
    WHERE profile.organization_id=p_tenant AND profile.agent_id=p_agent
      AND project.lifecycle='ACTIVE' AND agent.enabled AND agent.state<>'ARCHIVED'
      AND (p_authority_project IS NULL OR project.id=p_authority_project)
      AND control_plane.catalog_resource_visible(p_tenant,p_actor,'project.manage','PROJECT',
          project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())
      AND control_plane.catalog_resource_visible(p_tenant,p_actor,'agent.manage','AGENT',
          agent.id,project.id,agent.created_by,'{}'::jsonb,transaction_timestamp())
      AND EXISTS (
          SELECT 1 FROM control_plane.project_assistant_connection_purposes origin
          JOIN control_plane.integration_connections connection ON connection.id=origin.connection_id
            AND connection.organization_id=origin.organization_id AND connection.lifecycle_state='ACTIVE'
          WHERE origin.organization_id=p_tenant AND origin.project_ref=project.ref
            AND origin.profile_ref=profile.ref AND origin.assistant_ref=agent.ref
            AND control_plane.catalog_resource_visible(p_tenant,p_actor,'integration.manage','INTEGRATION',
                connection.id,NULL,connection.created_by,'{}'::jsonb,transaction_timestamp())))
    THEN ARRAY['CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT']::text[] ELSE '{}'::text[] END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.assistant_project_integration_grant_operations(uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.assistant_project_integration_grant_operations(uuid,uuid,uuid,text,uuid) TO control_plane_runtime;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'project assistant integration grants are forward-only'; END $$;
-- +goose StatementEnd
