-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.projects ADD CONSTRAINT projects_assistant_tenant UNIQUE (id, organization_id);
ALTER TABLE control_plane.agents ADD CONSTRAINT agents_assistant_tenant UNIQUE (id, organization_id);
ALTER TABLE control_plane.agents ADD CONSTRAINT agents_assistant_project UNIQUE (id, organization_id, project_id);

CREATE TABLE control_plane.project_assistant_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^asstp_[A-Za-z0-9_-]{8,90}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL,
    agent_id uuid NOT NULL UNIQUE,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, project_id),
    UNIQUE (id, organization_id, project_id, agent_id),
    FOREIGN KEY (project_id, organization_id) REFERENCES control_plane.projects(id, organization_id) DEFERRABLE INITIALLY IMMEDIATE,
    FOREIGN KEY (agent_id, organization_id, project_id) REFERENCES control_plane.agents(id, organization_id, project_id) DEFERRABLE INITIALLY IMMEDIATE
);

ALTER TABLE control_plane.assistant_conversations
    ADD COLUMN assistant_scope text NOT NULL DEFAULT 'SYSTEM' CHECK (assistant_scope IN ('SYSTEM', 'PROJECT')),
    ADD COLUMN assistant_agent_id uuid,
    ADD COLUMN assistant_profile_id uuid;

-- Однократная фиксация текущей серверной привязки, без legacy read path.
UPDATE control_plane.assistant_conversations conversation
SET assistant_agent_id = runtime.agent_id
FROM control_plane.assistant_runtime runtime
WHERE runtime.organization_id = conversation.organization_id;

UPDATE control_plane.sessions session
SET target_ref = agent.ref
FROM control_plane.assistant_conversations conversation
JOIN control_plane.agents agent ON agent.id = conversation.assistant_agent_id
WHERE session.id = conversation.session_id AND session.organization_id = conversation.organization_id;

UPDATE control_plane.runs run
SET target_ref = agent.ref
FROM control_plane.assistant_conversations conversation
JOIN control_plane.agents agent ON agent.id = conversation.assistant_agent_id
WHERE run.session_id = conversation.session_id AND run.organization_id = conversation.organization_id
  AND run.target_type = 'SYSTEM_ASSISTANT';

ALTER TABLE control_plane.assistant_conversations
    ALTER COLUMN assistant_agent_id SET NOT NULL,
    ALTER COLUMN assistant_scope DROP DEFAULT,
    ADD CONSTRAINT assistant_conversations_agent_tenant
        FOREIGN KEY (assistant_agent_id, organization_id) REFERENCES control_plane.agents(id, organization_id) DEFERRABLE INITIALLY IMMEDIATE,
    ADD CONSTRAINT assistant_conversations_profile_scope
        CHECK ((assistant_scope = 'SYSTEM' AND assistant_profile_id IS NULL)
            OR (assistant_scope = 'PROJECT' AND project_id IS NOT NULL AND assistant_profile_id IS NOT NULL)),
    ADD CONSTRAINT assistant_conversations_profile_pin
        FOREIGN KEY (assistant_profile_id, organization_id, project_id, assistant_agent_id)
        REFERENCES control_plane.project_assistant_profiles(id, organization_id, project_id, agent_id) DEFERRABLE INITIALLY IMMEDIATE;

-- Все карточки, диалоги и immutable runtime snapshots используют одинаковый
-- авторитетный gate. Глобальный помощник разрешает выбрать проект, но не
-- превращает членство в организации в право управления любым проектом.
-- +goose StatementBegin
CREATE FUNCTION control_plane.assistant_project_profile_creation_allowed(
    tenant uuid, actor uuid, selected_project uuid, assistant_scope text, context_kind text
) RETURNS boolean LANGUAGE sql STABLE SET search_path = pg_catalog, control_plane AS $$
SELECT assistant_scope='SYSTEM' AND context_kind IN ('','PROJECT')
  AND EXISTS (SELECT 1 FROM control_plane.subjects subject
      WHERE subject.id=actor AND subject.organization_id=tenant AND subject.active)
  AND ((selected_project IS NULL AND EXISTS (
      SELECT 1 FROM control_plane.memberships membership
      WHERE membership.organization_id=tenant AND membership.subject_id=actor
        AND membership.project_id IS NULL AND membership.active
        AND membership.role IN ('OWNER','ADMINISTRATOR')))
    OR EXISTS (
      SELECT 1 FROM control_plane.projects project
      WHERE project.organization_id=tenant AND project.lifecycle='ACTIVE'
        AND (selected_project IS NULL OR project.id=selected_project)
        AND NOT EXISTS (SELECT 1 FROM control_plane.project_assistant_profiles profile
            WHERE profile.organization_id=tenant AND profile.project_id=project.id)
        AND control_plane.catalog_resource_visible(tenant,actor,'project.manage','PROJECT',
            project.id,project.id,project.created_by,'{}'::jsonb,transaction_timestamp())));
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_assistant_conversation_profile()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, control_plane AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.assistant_scope IS DISTINCT FROM OLD.assistant_scope
        OR NEW.assistant_agent_id IS DISTINCT FROM OLD.assistant_agent_id
        OR NEW.assistant_profile_id IS DISTINCT FROM OLD.assistant_profile_id) THEN
        RAISE EXCEPTION 'assistant conversation profile is immutable' USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM control_plane.sessions session
        JOIN control_plane.agents agent ON agent.id = NEW.assistant_agent_id AND agent.organization_id = NEW.organization_id
        WHERE session.id = NEW.session_id AND session.organization_id = NEW.organization_id
          AND session.created_by = NEW.created_by AND session.project_id IS NOT DISTINCT FROM NEW.project_id
          AND session.target_type = 'SYSTEM_ASSISTANT' AND session.target_ref = agent.ref
          AND ((NEW.assistant_scope = 'SYSTEM' AND agent.system_key = 'system-assistant' AND agent.project_id IS NULL
                AND EXISTS (SELECT 1 FROM control_plane.assistant_runtime runtime
                            WHERE runtime.organization_id = NEW.organization_id AND runtime.agent_id = agent.id))
            OR (NEW.assistant_scope = 'PROJECT' AND agent.system_key IS NULL AND agent.project_id = NEW.project_id))
    ) THEN
        RAISE EXCEPTION 'assistant conversation profile boundary mismatch' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER assistant_conversation_profile_guard BEFORE INSERT OR UPDATE
ON control_plane.assistant_conversations FOR EACH ROW EXECUTE FUNCTION control_plane.guard_assistant_conversation_profile();

-- Новый профиль и четыре ребра pin входят в авторитетный граф удаления.
-- Отпечаток измерен на полностью мигрированной disposable PostgreSQL; неизвестное
-- отклонение графа или прежней функции не допускает обновления guard.
-- +goose StatementBegin
DO $migration$
DECLARE
    v_nodes integer;
    v_node_fingerprint text;
    v_edges integer;
    v_edge_fingerprint text;
    v_definition text;
    v_old_nodes constant text := 'IF v_nodes <> 96 OR v_node_fingerprint <> ''944cc88a40bfa8addb7930ac23c713ab'' THEN';
    v_new_nodes constant text := 'IF v_nodes <> 97 OR v_node_fingerprint <> ''02b472590335b8fb7370804b386cdf32'' THEN';
    v_old_edges constant text := 'IF v_edges <> 244 OR v_edge_fingerprint <> ''b487c79691a16a205f983e6d0adc5039'' THEN';
    v_new_edges constant text := 'IF v_edges <> 248 OR v_edge_fingerprint <> ''1d12e5e5a6c4475ccc79b868af564679'' THEN';
BEGIN
    WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid
        UNION
        SELECT constraint_row.conrelid FROM nodes
        JOIN pg_constraint constraint_row ON constraint_row.contype='f' AND constraint_row.confrelid=nodes.relation_id
        JOIN pg_namespace namespace_row ON namespace_row.oid=constraint_row.connamespace AND namespace_row.nspname='control_plane'
    )
    SELECT count(*), md5(string_agg(namespace_row.nspname || '.' || class_row.relname,
        E'\n' ORDER BY namespace_row.nspname,class_row.relname))
    INTO v_nodes,v_node_fingerprint FROM nodes
    JOIN pg_class class_row ON class_row.oid=nodes.relation_id
    JOIN pg_namespace namespace_row ON namespace_row.oid=class_row.relnamespace;

    WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid
        UNION
        SELECT constraint_row.conrelid FROM nodes
        JOIN pg_constraint constraint_row ON constraint_row.contype='f' AND constraint_row.confrelid=nodes.relation_id
        JOIN pg_namespace namespace_row ON namespace_row.oid=constraint_row.connamespace AND namespace_row.nspname='control_plane'
    )
    SELECT count(*), md5(string_agg(child_namespace.nspname || '.' || child_class.relname || '|' ||
        constraint_row.conname || '|' || parent_namespace.nspname || '.' || parent_class.relname || '|' || constraint_row.confdeltype::text,
        E'\n' ORDER BY child_namespace.nspname,child_class.relname,constraint_row.conname))
    INTO v_edges,v_edge_fingerprint FROM pg_constraint constraint_row
    JOIN pg_class child_class ON child_class.oid=constraint_row.conrelid
    JOIN pg_namespace child_namespace ON child_namespace.oid=child_class.relnamespace
    JOIN pg_class parent_class ON parent_class.oid=constraint_row.confrelid
    JOIN pg_namespace parent_namespace ON parent_namespace.oid=parent_class.relnamespace
    WHERE constraint_row.contype='f' AND constraint_row.confrelid IN (SELECT relation_id FROM nodes)
      AND constraint_row.conrelid IN (SELECT relation_id FROM nodes);
    IF v_nodes<>97 OR v_node_fingerprint<>'02b472590335b8fb7370804b386cdf32'
       OR v_edges<>248 OR v_edge_fingerprint<>'1d12e5e5a6c4475ccc79b868af564679' THEN
        RAISE EXCEPTION 'project assistant purge graph migration precondition failed';
    END IF;
    SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO v_definition;
    IF v_definition IS NULL OR strpos(v_definition,v_old_nodes)=0 OR strpos(v_definition,v_old_edges)=0
       OR strpos(substr(v_definition,strpos(v_definition,v_old_nodes)+length(v_old_nodes)),v_old_nodes)>0
       OR strpos(substr(v_definition,strpos(v_definition,v_old_edges)+length(v_old_edges)),v_old_edges)>0 THEN
        RAISE EXCEPTION 'project assistant purge function migration precondition failed';
    END IF;
    EXECUTE replace(replace(v_definition,v_old_nodes,v_new_nodes),v_old_edges,v_new_edges);
END;
$migration$;
-- +goose StatementEnd

RESET ROLE;

-- Миграции только forward-only; откат выполняется новой migration.
-- +goose Down
SELECT 1;
