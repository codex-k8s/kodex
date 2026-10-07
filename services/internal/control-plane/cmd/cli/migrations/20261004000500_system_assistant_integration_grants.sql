-- +goose Up
SET ROLE control_plane_owner;

-- Закрытая идентичность организационного SYSTEM run не выводится из NULL project.
-- +goose StatementBegin
CREATE FUNCTION control_plane.owned_organization_assistant_run(p_organization uuid,p_run uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
SELECT EXISTS (
    SELECT 1 FROM control_plane.runs run
    JOIN control_plane.sessions session ON session.id=run.session_id AND session.organization_id=run.organization_id
    JOIN control_plane.assistant_conversations conversation ON conversation.session_id=session.id
      AND conversation.organization_id=run.organization_id AND conversation.project_id IS NULL
    JOIN control_plane.agents agent ON agent.id=conversation.assistant_agent_id AND agent.organization_id=run.organization_id
    JOIN control_plane.assistant_runtime runtime ON runtime.agent_id=agent.id AND runtime.organization_id=agent.organization_id
    WHERE run.organization_id=p_organization AND run.id=p_run AND run.project_id IS NULL
      AND run.source='SYSTEM_ASSISTANT' AND run.target_type='SYSTEM_ASSISTANT'
      AND conversation.assistant_scope='SYSTEM' AND conversation.assistant_profile_id IS NULL
      AND agent.project_id IS NULL AND agent.system_key='system-assistant'
      AND agent.ref=run.target_ref AND agent.ref=session.target_ref
);
$$;
-- +goose StatementEnd

ALTER TABLE control_plane.owner_gates ADD COLUMN scope_kind text;
UPDATE control_plane.owner_gates SET scope_kind='PROJECT' WHERE project_id IS NOT NULL;
ALTER TABLE control_plane.owner_gates
    ALTER COLUMN project_id DROP NOT NULL,
    ALTER COLUMN scope_kind SET NOT NULL,
    ADD CONSTRAINT owner_gates_scope_check CHECK (
      (scope_kind='PROJECT' AND project_id IS NOT NULL) OR
      (scope_kind='ORGANIZATION' AND project_id IS NULL)
    );

-- PostgreSQL owner назначает scope из exact сохранённого графа; payload scope не является authority.
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_owner_gate_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected_scope text;
BEGIN
    IF TG_OP='UPDATE' AND (NEW.organization_id,NEW.project_id,NEW.root_run_id,NEW.node_id,NEW.scope_kind)
      IS DISTINCT FROM (OLD.organization_id,OLD.project_id,OLD.root_run_id,OLD.node_id,OLD.scope_kind) THEN
        RAISE EXCEPTION 'owner gate scope is immutable';
    END IF;
    IF EXISTS (SELECT 1 FROM control_plane.runs run JOIN control_plane.projects project
        ON project.id=run.project_id AND project.organization_id=run.organization_id
        WHERE run.id=NEW.root_run_id AND run.organization_id=NEW.organization_id
          AND run.project_id=NEW.project_id) THEN
        expected_scope:='PROJECT';
    ELSIF NEW.project_id IS NULL AND control_plane.owned_organization_assistant_run(NEW.organization_id,NEW.root_run_id) THEN
        expected_scope:='ORGANIZATION';
    ELSE RAISE EXCEPTION 'owner gate runtime owner mismatch'; END IF;
    IF NEW.scope_kind IS NOT NULL AND NEW.scope_kind<>expected_scope THEN
        RAISE EXCEPTION 'owner gate scope mismatch';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM control_plane.run_nodes node WHERE node.id=NEW.node_id
        AND node.organization_id=NEW.organization_id AND node.root_run_id=NEW.root_run_id) THEN
        RAISE EXCEPTION 'owner gate node owner mismatch';
    END IF;
    NEW.scope_kind:=expected_scope;
    RETURN NEW;
END;
$$;
CREATE TRIGGER protect_owner_gate_scope BEFORE INSERT OR UPDATE ON control_plane.owner_gates
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_owner_gate_scope();
-- +goose StatementEnd

ALTER TABLE control_plane.integration_approval_scopes ALTER COLUMN project_id DROP NOT NULL;
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_integration_approval_scope_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM control_plane.owner_gates gate
        JOIN control_plane.runs run ON run.id=gate.root_run_id AND run.organization_id=gate.organization_id
        JOIN control_plane.integration_invocations invocation ON invocation.id=gate.integration_invocation_id
          AND invocation.organization_id=gate.organization_id
        JOIN control_plane.run_nodes node ON node.id=invocation.node_id AND node.organization_id=gate.organization_id
        WHERE gate.id=NEW.origin_gate_id AND gate.organization_id=NEW.organization_id
          AND gate.root_run_id=NEW.root_run_id AND run.id=NEW.root_run_id
          AND gate.project_id IS NOT DISTINCT FROM NEW.project_id
          AND run.project_id IS NOT DISTINCT FROM NEW.project_id
          AND node.agent_id=NEW.agent_id AND invocation.connection_id=NEW.connection_id AND invocation.grant_id=NEW.grant_id
          AND (gate.scope_kind='PROJECT' OR (gate.scope_kind='ORGANIZATION'
              AND control_plane.owned_organization_assistant_run(NEW.organization_id,NEW.root_run_id)))) THEN
        RAISE EXCEPTION 'integration approval scope owner mismatch';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER protect_integration_approval_scope_owner BEFORE INSERT OR UPDATE ON control_plane.integration_approval_scopes
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_integration_approval_scope_owner();
-- +goose StatementEnd

GRANT EXECUTE ON FUNCTION control_plane.owned_organization_assistant_run(uuid,uuid) TO control_plane_runtime;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'system assistant integration scope migration is forward-only'; END $$;
-- +goose StatementEnd
