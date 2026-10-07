-- +goose Up
SET ROLE control_plane_owner;

-- Один fresh eligibility rule для создания, read/rejoin и runtime-проекции.
-- Выбранный UI-проект не является signed project authority.
-- +goose StatementBegin
CREATE FUNCTION control_plane.assistant_system_integration_grant_operations(
    tenant uuid, actor uuid, assistant_agent uuid, assistant_scope text, authority_project uuid
) RETURNS text[] LANGUAGE sql STABLE SECURITY INVOKER
SET search_path = pg_catalog, control_plane AS $$
SELECT CASE WHEN assistant_scope='SYSTEM' AND authority_project IS NULL
  AND EXISTS (
      SELECT 1 FROM control_plane.assistant_runtime runtime
      JOIN control_plane.agents agent ON agent.id=runtime.agent_id AND agent.organization_id=runtime.organization_id
      WHERE runtime.organization_id=tenant AND agent.id=assistant_agent
        AND agent.system_key='system-assistant' AND agent.project_id IS NULL
        AND agent.enabled AND agent.state<>'ARCHIVED')
  AND EXISTS (
      SELECT 1 FROM control_plane.memberships membership
      JOIN control_plane.subjects subject ON subject.id=membership.subject_id AND subject.organization_id=tenant AND subject.active
      WHERE membership.organization_id=tenant AND membership.subject_id=actor
        AND membership.project_id IS NULL AND membership.active
        AND membership.role IN ('OWNER','ADMINISTRATOR'))
  AND control_plane.catalog_resource_visible(tenant,actor,'organization.manage','ORGANIZATION',
      tenant,NULL,NULL,'{}'::jsonb,transaction_timestamp())
  THEN ARRAY['CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT']::text[] ELSE '{}'::text[] END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.assistant_system_integration_grant_operations(uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.assistant_system_integration_grant_operations(uuid,uuid,uuid,text,uuid) TO control_plane_runtime;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'system assistant grant operation projection is forward-only';
END $$;
-- +goose StatementEnd
