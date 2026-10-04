-- +goose Up
SET ROLE control_plane_owner;

-- Connection остаётся ресурсом организации. Историческая цель не является
-- grant и не вводит FK, способный включить общий ресурс в purge проекта.
CREATE TABLE control_plane.project_assistant_connection_purposes (
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid PRIMARY KEY REFERENCES control_plane.integration_connections(id) ON DELETE CASCADE,
    project_ref text NOT NULL CHECK (project_ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    profile_ref text NOT NULL CHECK (profile_ref ~ '^asstp_[A-Za-z0-9_-]{8,90}$'),
    assistant_ref text NOT NULL CHECK (assistant_ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    profile_version bigint NOT NULL CHECK (profile_version>0),
    agent_version bigint NOT NULL CHECK (agent_version>0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
GRANT SELECT,INSERT,DELETE ON control_plane.project_assistant_connection_purposes TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_project_assistant_connection_purpose() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
BEGIN
  IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'project assistant connection purpose is immutable'; END IF;
  IF NOT EXISTS (SELECT 1 FROM control_plane.integration_connections connection
      JOIN control_plane.project_assistant_profiles profile ON profile.organization_id=connection.organization_id
      JOIN control_plane.projects project ON project.id=profile.project_id AND project.organization_id=profile.organization_id
      JOIN control_plane.agents agent ON agent.id=profile.agent_id AND agent.organization_id=profile.organization_id AND agent.project_id=profile.project_id
      WHERE connection.id=NEW.connection_id AND connection.organization_id=NEW.organization_id
        AND project.ref=NEW.project_ref AND project.lifecycle='ACTIVE' AND profile.ref=NEW.profile_ref AND profile.version=NEW.profile_version
        AND agent.ref=NEW.assistant_ref AND agent.version=NEW.agent_version AND agent.enabled AND agent.state<>'ARCHIVED'
        AND agent.system_key IS NULL) THEN RAISE EXCEPTION 'project assistant connection owner is invalid'; END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER project_assistant_connection_purpose_guard BEFORE INSERT OR UPDATE
ON control_plane.project_assistant_connection_purposes FOR EACH ROW EXECUTE FUNCTION control_plane.guard_project_assistant_connection_purpose();

-- Один owner predicate для initial projection, read/rejoin и immutable claim.
-- Здесь разрешена только подготовка плана; effect требует отдельный owner apply.
-- +goose StatementBegin
CREATE FUNCTION control_plane.assistant_project_connection_operations(tenant uuid, actor uuid, assistant_agent uuid, assistant_scope text, authority_project uuid)
RETURNS text[] LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
SELECT CASE WHEN assistant_scope='PROJECT'
  AND EXISTS (SELECT 1 FROM control_plane.project_assistant_profiles profile
      JOIN control_plane.projects project ON project.id=profile.project_id AND project.organization_id=profile.organization_id
      JOIN control_plane.agents agent ON agent.id=profile.agent_id AND agent.organization_id=profile.organization_id AND agent.project_id=profile.project_id
      WHERE profile.organization_id=tenant AND agent.id=assistant_agent AND project.lifecycle='ACTIVE'
        AND agent.enabled AND agent.state<>'ARCHIVED' AND agent.system_key IS NULL
        AND (authority_project IS NULL OR authority_project=project.id)
        AND control_plane.catalog_resource_visible(tenant,actor,'project.manage','PROJECT',project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp()))
  AND EXISTS (SELECT 1 FROM control_plane.memberships member JOIN control_plane.subjects subject
      ON subject.id=member.subject_id AND subject.organization_id=tenant AND subject.active
      WHERE member.organization_id=tenant AND member.subject_id=actor AND member.project_id IS NULL
        AND member.active AND member.role IN ('OWNER','ADMINISTRATOR'))
  AND control_plane.catalog_resource_visible(tenant,actor,'organization.manage','ORGANIZATION',tenant,NULL,NULL,'{}'::jsonb,transaction_timestamp())
  THEN ARRAY['PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION']::text[] ELSE '{}'::text[] END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.assistant_project_connection_operations(uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.assistant_project_connection_operations(uuid,uuid,uuid,text,uuid) TO control_plane_runtime;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'project assistant connection purpose is forward-only'; END $$;
-- +goose StatementEnd
